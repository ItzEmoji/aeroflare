package shared

import (
	"fmt"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/charmbracelet/huh"
)

// githubClientID identifies the Aeroflare OAuth app used for the GitHub
// device authorization flow below; it's a public client ID, not a secret.
const githubClientID = "Ov23liIJyLpd2Cse5gne"

// RunInteractiveGithubAuth prompts the user to choose between the OAuth
// device flow (opens a browser) or pasting a personal access token
// manually, then saves whichever token results via the secrets manager.
func RunInteractiveGithubAuth(f *cmdutil.Factory) (string, error) {
	manager := f.Secrets()
	var ghMethod string
	err := huh.NewSelect[string]().
		Title("How would you like to authenticate with GitHub?").
		Options(
			huh.NewOption("Device Auth Flow (Browser)", "device"),
			huh.NewOption("Enter Token Manually", "manual"),
		).
		Value(&ghMethod).
		WithTheme(ui.AeroflareTheme()).
		Run()
	if err != nil {
		return "", cmdutil.ErrCancel
	}

	var token string
	if ghMethod == "device" {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Requesting device code...")
		res, err := auth.RequestDeviceCode(githubClientID)
		if err != nil {
			return "", fmt.Errorf("failed to request code: %w", err)
		}
		_, _ = fmt.Fprintf(f.IOStreams.Out, "Please go to %s and enter the code: %s\n", res.VerificationURI, res.UserCode)
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Waiting for authorization...")

		token, err = auth.PollAccessToken(githubClientID, res.DeviceCode, res.Interval)
		if err != nil {
			return "", fmt.Errorf("authorization failed: %w", err)
		}
	} else {
		err = huh.NewInput().Title("GitHub Token").EchoMode(huh.EchoModePassword).Value(&token).WithTheme(ui.AeroflareTheme()).Run()
		if err != nil {
			return "", cmdutil.ErrCancel
		}
	}

	if token != "" {
		if err := manager.Set("github-token", token); err != nil {
			return "", fmt.Errorf("failed to save token: %w", err)
		}
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Success! GitHub token saved. This will automatically be used for GitHub APIs and the ghcr.io container registry.")
	}
	return token, nil
}

// RunInteractiveGitlabAuth prompts for a GitLab personal access token and
// saves it via the secrets manager.
func RunInteractiveGitlabAuth(f *cmdutil.Factory) (string, error) {
	manager := f.Secrets()
	var token string
	err := huh.NewInput().Title("GitLab Personal Access Token").EchoMode(huh.EchoModePassword).Value(&token).WithTheme(ui.AeroflareTheme()).Run()
	if err != nil {
		return "", cmdutil.ErrCancel
	}
	if token != "" {
		if err := manager.Set("gitlab-token", token); err != nil {
			return "", fmt.Errorf("failed to save token: %w", err)
		}
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Success! GitLab token saved.")
	}
	return token, nil
}

// RunInteractiveCloudflareAuth prompts for a Cloudflare API token and
// account ID and saves whichever of the two the user entered.
func RunInteractiveCloudflareAuth(f *cmdutil.Factory) (string, string, error) {
	manager := f.Secrets()
	var apiToken, accountID string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Cloudflare API Token").EchoMode(huh.EchoModePassword).Value(&apiToken),
			huh.NewInput().Title("Cloudflare Account ID").Value(&accountID),
		),
	).WithTheme(ui.AeroflareTheme()).Run()
	if err != nil {
		return "", "", cmdutil.ErrCancel
	}

	if apiToken != "" {
		if err := manager.Set("cf-token", apiToken); err != nil {
			return "", "", fmt.Errorf("failed to save Cloudflare API token: %w", err)
		}
	}
	if accountID != "" {
		if err := manager.Set("cf-account-id", accountID); err != nil {
			return "", "", fmt.Errorf("failed to save Cloudflare account ID: %w", err)
		}
	}
	if apiToken != "" || accountID != "" {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Cloudflare credentials saved.")
	}
	return apiToken, accountID, nil
}

// RunInteractiveOCIAuth prompts for a registry hostname (if not already
// known) plus a username/token pair, and saves them under
// "oci-<registry>-username" / "oci-<registry>-token".
func RunInteractiveOCIAuth(f *cmdutil.Factory, registry string) (string, string, error) {
	manager := f.Secrets()
	var user, pass string

	if registry == "" {
		err := huh.NewInput().Title("Registry URL (e.g. registry.gitlab.com)").Value(&registry).WithTheme(ui.AeroflareTheme()).Run()
		if err != nil || registry == "" {
			return "", "", cmdutil.ErrCancel
		}
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Username for "+registry).Value(&user),
			huh.NewInput().Title("Token / Password").EchoMode(huh.EchoModePassword).Value(&pass),
		),
	).WithTheme(ui.AeroflareTheme()).Run()
	if err != nil {
		return "", "", cmdutil.ErrCancel
	}

	if registry != "" {
		if err := manager.Set(fmt.Sprintf("oci-%s-username", registry), user); err != nil {
			return "", "", fmt.Errorf("failed to save OCI username: %w", err)
		}
		if err := manager.Set(fmt.Sprintf("oci-%s-token", registry), pass); err != nil {
			return "", "", fmt.Errorf("failed to save OCI token: %w", err)
		}
		_, _ = fmt.Fprintln(f.IOStreams.Out, "OCI credentials saved.")
	}
	return user, pass, nil
}

// PromptServiceFields interactively prompts for each of a service's fields,
// masking input for secret fields, and returns the entered values keyed by
// field Name. It is the catalog-driven prompt used by `auth set <service>`
// when no values are given on the command line.
func PromptServiceFields(svc auth.Service) map[string]string {
	vals := make(map[string]string)
	for _, field := range svc.Fields {
		var val string
		input := huh.NewInput().Title(field.Label).Value(&val)
		if field.Secret {
			input = input.EchoMode(huh.EchoModePassword)
		}
		if err := input.WithTheme(ui.AeroflareTheme()).Run(); err != nil {
			return vals
		}
		if val != "" {
			vals[field.Name] = val
		}
	}
	return vals
}

// RunInteractiveAuth is the entry point for `aeroflare auth login` when no
// token flags were passed: it asks which service to authenticate and
// dispatches to the matching RunInteractive*Auth helper.
func RunInteractiveAuth(f *cmdutil.Factory) error {
	var service string
	err := huh.NewSelect[string]().
		Title("What do you want to authenticate?").
		Options(
			huh.NewOption("GitHub", "github"),
			huh.NewOption("GitLab", "gitlab"),
			huh.NewOption("Cloudflare", "cloudflare"),
			huh.NewOption("Custom OCI Registry", "oci"),
		).
		Value(&service).
		WithTheme(ui.AeroflareTheme()).
		Run()

	if err != nil {
		return cmdutil.ErrCancel
	}

	switch service {
	case "github":
		_, err = RunInteractiveGithubAuth(f)
	case "gitlab":
		_, err = RunInteractiveGitlabAuth(f)
	case "cloudflare":
		_, _, err = RunInteractiveCloudflareAuth(f)
	case "oci":
		_, _, err = RunInteractiveOCIAuth(f, "")
	}
	return err
}
