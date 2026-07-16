package setup

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/charmbracelet/huh"
	"github.com/spf13/viper"
)

// RunWizard collects all configuration from the user through an interactive wizard.
// No infrastructure changes are made during this phase.
func RunWizard(f *cmdutil.Factory) (*InitConfig, error) {
	fmt.Println()
	fmt.Println("  \u2726 Aeroflare Setup")
	fmt.Println()

	cfg := &InitConfig{}

	if err := promptCoreSettings(cfg); err != nil {
		return nil, err
	}

	cfg.DeriveDefaults()

	if err := promptCredentials(f, cfg); err != nil {
		return nil, err
	}

	if err := promptWorkerToken(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// promptWorkerToken optionally collects a registry token to embed in the Worker
// (stored as the NIXCACHE_TOKEN secret). It is entirely optional: a public cache
// needs no token, but a private cache — or anyone who wants the Worker to skip
// GHCR's token exchange for lower latency — can supply one. When the common
// ghcr.io + GitHub path already yielded a PAT, we offer to reuse it instead of
// asking the user to paste a token again.
func promptWorkerToken(cfg *InitConfig) error {
	// A value supplied via flag/config is used as-is, no prompt.
	if t := viper.GetString("worker-token"); t != "" {
		cfg.WorkerToken = t
		return nil
	}

	// The direct-bearer optimization (and the base64 encoding we apply when
	// storing the secret) is GHCR-specific. Other registries don't accept a
	// base64 credential as a bearer, so they always use the cached token
	// exchange and we don't offer this prompt for them.
	if cfg.Registry != "ghcr.io" {
		return nil
	}

	// On the common ghcr.io + GitHub path the PAT doubles as the registry
	// credential, so offer to reuse it instead of asking for another token.
	reusable := cfg.GitToken

	desc := "Lets the Worker reach private repos and skip GHCR's token exchange (faster). Skip for a public cache."
	if reusable != "" {
		desc = "Reuse your existing token so the Worker can reach private repos and skip GHCR's token exchange (faster). Skip for a public cache."
	}

	var wantToken bool
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Store a registry token on the Worker? (optional)").
				Description(desc).
				Value(&wantToken),
		),
	).WithTheme(AeroflareTheme()).Run(); err != nil {
		return fmt.Errorf("wizard cancelled")
	}

	if !wantToken {
		return nil
	}

	if reusable != "" {
		cfg.WorkerToken = reusable
		return nil
	}

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Worker registry token").
				Description("A PAT with read:packages. Stored as the NIXCACHE_TOKEN Worker secret.").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.WorkerToken).
				Validate(notEmpty("Worker registry token")),
		),
	).WithTheme(AeroflareTheme()).Run(); err != nil {
		return fmt.Errorf("wizard cancelled")
	}
	return nil
}

// promptCoreSettings asks for cache name, registry and git provider. For each
// setting, a value already supplied via CLI flag / config (read through viper)
// is used as-is and no prompt is shown for it; only settings left unspecified
// are added to the interactive form.
func promptCoreSettings(cfg *InitConfig) error {
	var gitProvider string

	cacheURL := viper.GetString("cache-url")
	cacheName := viper.GetString("cache")
	registryVal := viper.GetString("registry")

	if cacheURL != "" {
		if strings.HasPrefix(cacheURL, "oci://") {
			u, err := url.Parse(cacheURL)
			if err != nil {
				return fmt.Errorf("invalid cache-url: %w", err)
			}
			if u.Host != "" {
				cfg.Registry = u.Host
				cfg.CacheName = strings.TrimPrefix(u.Path, "/")
			} else {
				cfg.Registry = "custom"
				cfg.CacheName = cacheURL
			}
		} else {
			cfg.Registry = "custom"
			cfg.CacheName = cacheURL
		}
	} else if cacheName != "" {
		cfg.CacheName = cacheName
		cfg.Registry = "ghcr.io"
	} else {
		cfg.Registry = "ghcr.io"
	}

	if registryVal != "" {
		cfg.Registry = registryVal
	}

	gitProviderVal := viper.GetString("git-provider")
	if gitProviderVal != "" {
		if gitProviderVal != "none" && gitProviderVal != "github" && gitProviderVal != "gitlab" {
			return fmt.Errorf("invalid git provider configured: %s. Must be 'none', 'github', or 'gitlab'", gitProviderVal)
		}
		gitProvider = gitProviderVal
	}

	var groups []*huh.Group
	var coreFields []huh.Field

	if cfg.CacheName == "" {
		coreFields = append(coreFields, huh.NewInput().
			Title("Cache name").
			Description("A unique name for your binary cache (e.g. myuser/my-cache)").
			Value(&cfg.CacheName).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("cache name is required")
				}
				return nil
			}))
	}

	if cacheURL == "" && registryVal == "" && cacheName == "" {
		coreFields = append(coreFields, huh.NewInput().
			Title("OCI registry").
			Description("Container registry for storing cache data").
			Value(&cfg.Registry))
	}

	if len(coreFields) > 0 {
		groups = append(groups, huh.NewGroup(coreFields...))
	}

	var secondaryFields []huh.Field
	if gitProviderVal == "" {
		secondaryFields = append(secondaryFields, huh.NewSelect[string]().
			Title("Git integration").
			Description("Connect a Git repository for automatic CI/CD deployments?").
			Options(
				huh.NewOption("None", "none"),
				huh.NewOption("GitHub", "github"),
				huh.NewOption("GitLab", "gitlab"),
			).
			Value(&gitProvider))
	}

	if len(secondaryFields) > 0 {
		groups = append(groups, huh.NewGroup(secondaryFields...))
	}

	if len(groups) > 0 {
		err := huh.NewForm(groups...).WithTheme(AeroflareTheme()).Run()
		if err != nil {
			return fmt.Errorf("wizard cancelled")
		}
	}

	cfg.GitProvider = GitProvider(gitProvider)
	return nil
}

// promptCredentials collects the credentials required by the selected options.
//
// Every credential is obtained through the auth module (pkg/cmd/auth/shared),
// which owns the whole chain: resolve from flag/env/secrets, prompt only for
// what's missing, and persist whatever it collects. The wizard must not grow
// its own prompting or device-flow logic — a second, non-persisting copy is
// what previously made `init` authenticate twice on a fresh machine.
func promptCredentials(f *cmdutil.Factory, cfg *InitConfig) error {
	seedOverridesFromConfig(f, cfg.GitProvider)

	// Cloudflare credentials are always required (we deploy a Worker).
	var err error
	cfg.CloudflareToken, cfg.CloudflareAccountID, err = shared.RequireCloudflareToken(f)
	if err != nil {
		return err
	}

	switch cfg.GitProvider {
	case GitGitHub:
		cfg.GitToken, err = shared.RequireGithubToken(f)
	case GitGitLab:
		cfg.GitToken, err = shared.RequireGitlabToken(f)
	}
	if err != nil {
		return err
	}

	// A separate OCI credential is only needed when the registry isn't the
	// git provider's own registry (ghcr.io+GitHub or registry.gitlab.com+GitLab),
	// since in those cases the git token doubles as the OCI token (see
	// createOCIRepository's explicitToken handling).
	needsOCIToken := (cfg.Registry != "ghcr.io" || cfg.GitProvider != GitGitHub) &&
		(cfg.Registry != "registry.gitlab.com" || cfg.GitProvider != GitGitLab)

	// The well-known registries authenticate with their provider's token rather
	// than a username/password pair, and reach here only when that provider
	// wasn't chosen for git (otherwise GitToken above already covers them).
	if needsOCIToken {
		switch cfg.Registry {
		case "ghcr.io":
			cfg.OCIToken, err = shared.RequireGithubToken(f)
		case "registry.gitlab.com":
			cfg.OCIToken, err = shared.RequireGitlabToken(f)
		default:
			_, cfg.OCIToken, err = shared.RequireOCIToken(f, cfg.Registry)
		}
		if err != nil {
			return err
		}
	}

	// Resolve Git username from token.
	if cfg.GitProvider != GitNone {
		var err error
		switch cfg.GitProvider {
		case GitGitHub:
			cfg.GitUsername, err = getGitHubUsername(cfg.GitToken)
		case GitGitLab:
			cfg.GitUsername, err = getGitLabUsername(cfg.GitToken)
		}
		if err != nil {
			return fmt.Errorf("could not fetch %s username: %w", cfg.GitProvider, err)
		}
	}

	return nil
}

// seedOverridesFromConfig copies credentials from the config file (the keys
// `aeroflare settings` writes) into the factory's flag overrides, unless the
// matching flag was already passed. The auth module resolves overrides ahead of
// the environment and secrets manager, so this is what gives a configured
// token the priority a flag has, without the wizard reading credentials itself.
func seedOverridesFromConfig(f *cmdutil.Factory, provider GitProvider) {
	if f.Overrides.CfToken == "" {
		f.Overrides.CfToken = viper.GetString("cloudflare-api-token")
	}
	if f.Overrides.CfUserID == "" {
		f.Overrides.CfUserID = viper.GetString("cloudflare-account-id")
	}

	// One `git-token` key serves whichever provider is configured.
	gitToken := viper.GetString("git-token")
	if gitToken == "" {
		return
	}
	switch provider {
	case GitGitHub:
		if f.Overrides.GithubToken == "" {
			f.Overrides.GithubToken = gitToken
		}
	case GitGitLab:
		if f.Overrides.GitlabToken == "" {
			f.Overrides.GitlabToken = gitToken
		}
	}
}

// DisplaySummary shows a configuration summary and asks for confirmation.
func DisplaySummary(cfg *InitConfig) (bool, error) {
	fields := []ui.BoxField{
		{Label: "Cache", Value: cfg.CacheName},
		{Label: "Registry", Value: cfg.Registry},
		{Label: "Repository", Value: cfg.Repository},
		{Label: "Worker", Value: cfg.WorkerName},
	}
	if cfg.GitProvider != GitNone {
		fields = append(fields, ui.BoxField{Label: "Git", Value: fmt.Sprintf("%s (%s)", cfg.GitProvider, cfg.GitUsername)})
	}
	workerToken := "none (anonymous)"
	if cfg.WorkerToken != "" {
		workerToken = "configured (private/faster)"
	}
	fields = append(fields, ui.BoxField{Label: "Worker token", Value: workerToken})

	ui.PrintSummaryBox("Summary", fields)

	var confirmed bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Proceed with setup?").
				Affirmative("Yes, create resources").
				Negative("Cancel").
				Value(&confirmed),
		),
	).WithTheme(AeroflareTheme()).Run()
	if err != nil {
		return false, nil
	}
	return confirmed, nil
}

func notEmpty(name string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}
