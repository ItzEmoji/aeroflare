// Package settings implements `aeroflare settings`, an interactive terminal
// UI (using huh) for configuring themes, registry logins, and custom caching
// URLs.
package settings

import (
	"fmt"

	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// Options holds the dependencies settingsRun needs.
type Options struct {
	IO *iostreams.IOStreams
}

// NewCmdSettings builds the `aeroflare settings` command.
func NewCmdSettings(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Configure local preferences (theme, logins, cache URL)",
		Long: `Configure local, per-machine preferences -- appearance theme,
registry logins, and the default cache URL -- and save them to aeroflare.yaml.

These settings are local to this machine. To configure the remote cache itself
(such as its signing public key), use "aeroflare configure".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return settingsRun(f, opts)
		},
	}

	return cmd
}

func settingsRun(f *cmdutil.Factory, opts *Options) error {
	v, err := f.Config()
	if err != nil {
		return err
	}

	// Define variables to hold the form state.
	var theme string
	var registryAction string
	var cloudflareToken string
	var githubToken string
	var gitlabToken string
	var customRegistryURL string

	// Preload existing configuration from Viper to populate the form defaults.
	// If a user has previously configured Aeroflare, we want the interactive menu
	// to reflect their current choices.
	theme = v.GetString("theme")
	if theme == "" {
		theme = "default"
	}

	cloudflareToken = v.GetString("cloudflare-api-token")
	gitProvider := v.GetString("git-provider")
	if gitProvider == "" {
		gitProvider = "none"
	}

	// Determine the currently active registry action based on the saved config.
	// This sets the default selection in the "Registry Login & Setup" dropdown.
	if gitProvider == "github" {
		githubToken = v.GetString("git-token")
		registryAction = "github"
	} else if gitProvider == "gitlab" {
		gitlabToken = v.GetString("git-token")
		registryAction = "gitlab"
	} else if v.GetString("cache-url") != "" {
		registryAction = "custom"
		customRegistryURL = v.GetString("cache-url")
	} else if cloudflareToken != "" {
		registryAction = "cloudflare"
	} else {
		registryAction = "none"
	}

	// Build and run the primary configuration form.
	// This form captures general appearance settings and determines
	// which authentication flow (if any) the user wants to proceed with.
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Appearance Theme").
				Options(
					huh.NewOption("Dracula", "dracula"),
					huh.NewOption("Catppuccin", "catppuccin"),
					huh.NewOption("Gruvbox Dark", "gruvbox-dark"),
					huh.NewOption("Gruvbox Light", "gruvbox-light"),
					huh.NewOption("Default Terminal", "default"),
				).
				Value(&theme),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Registry Login & Setup").
				Description("Configure authentication for a registry").
				Options(
					huh.NewOption("GitHub Packages (ghcr.io)", "github"),
					huh.NewOption("GitLab Registry", "gitlab"),
					huh.NewOption("Cloudflare (Workers)", "cloudflare"),
					huh.NewOption("Custom OCI Registry", "custom"),
					huh.NewOption("None", "none"),
				).
				Value(&registryAction),
		),
	).WithTheme(ui.AeroflareTheme()).Run()

	// If the user aborts the form (e.g. by pressing Ctrl+C), cancel gracefully.
	if err != nil {
		return cmdutil.ErrCancel
	}

	// Based on the user's choice in the primary form, we dynamically construct
	// and present a secondary form to collect the necessary authentication credentials.
	var authGroups []*huh.Group
	switch registryAction {
	case "github":
		authGroups = append(authGroups, huh.NewGroup(
			huh.NewInput().
				Title("GitHub Personal Access Token").
				EchoMode(huh.EchoModePassword).
				Value(&githubToken),
		))
	case "gitlab":
		authGroups = append(authGroups, huh.NewGroup(
			huh.NewInput().
				Title("GitLab Personal Access Token").
				EchoMode(huh.EchoModePassword).
				Value(&gitlabToken),
		))
	case "cloudflare":
		authGroups = append(authGroups, huh.NewGroup(
			huh.NewInput().
				Title("Cloudflare API Token").
				EchoMode(huh.EchoModePassword).
				Value(&cloudflareToken),
		))
	case "custom":
		authGroups = append(authGroups, huh.NewGroup(
			huh.NewInput().
				Title("Registry URL (e.g., https://registry.example.com)").
				Value(&customRegistryURL),
		))
	}

	// If an authentication method was selected, run the secondary form.
	if len(authGroups) > 0 {
		err = huh.NewForm(authGroups...).WithTheme(ui.AeroflareTheme()).Run()
		if err != nil {
			return cmdutil.ErrCancel
		}
	}

	// Apply the captured settings back into the Viper configuration instance.
	v.Set("theme", theme)

	switch registryAction {
	case "github":
		v.Set("git-provider", "github")
		if githubToken != "" {
			v.Set("git-token", githubToken)
		}
	case "gitlab":
		v.Set("git-provider", "gitlab")
		if gitlabToken != "" {
			v.Set("git-token", gitlabToken)
		}
	case "cloudflare":
		if cloudflareToken != "" {
			v.Set("cloudflare-api-token", cloudflareToken)
		}
	case "custom":
		if customRegistryURL != "" {
			v.Set("cache-url", customRegistryURL)
		}
	}

	// Finally, persist the updated configuration to the disk (aeroflare.yaml).
	if err := v.WriteConfig(); err != nil {
		opts.IO.Error(fmt.Sprintf("Failed to save settings: %v", err))
	} else {
		opts.reportSaved(f.IsNewConfig(), v.ConfigFileUsed())
	}

	return nil
}

// reportSaved announces where the config landed, distinguishing a config file
// created fresh on this run from one that already existed and was updated.
func (opts *Options) reportSaved(isNewConfig bool, configFile string) {
	if isNewConfig {
		opts.IO.Success(fmt.Sprintf("Initial config has been saved to %s", configFile))
		return
	}
	opts.IO.Success(fmt.Sprintf("Config has been updated in %s", configFile))
}
