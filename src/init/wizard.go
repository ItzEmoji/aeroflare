package setup

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/viper"
	"aeroflare/src/ui"
)

// RunWizard collects all configuration from the user through an interactive wizard.
// No infrastructure changes are made during this phase.
func RunWizard() (*InitConfig, error) {
	fmt.Println()
	fmt.Println("  \u2726 Aeroflare Setup")
	fmt.Println()

	cfg := &InitConfig{}

	if err := promptCoreSettings(cfg); err != nil {
		return nil, err
	}

	cfg.DeriveDefaults()

	if err := promptCredentials(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// promptCoreSettings asks for cache name, registry, backend and git provider.
func promptCoreSettings(cfg *InitConfig) error {
	var backend string
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

	backendVal := viper.GetString("backend")
	if backendVal != "" {
		if backendVal != "r2" && backendVal != "native" && backendVal != "oci" {
			return fmt.Errorf("invalid backend configured: %s. Must be 'r2', 'native', or 'oci'", backendVal)
		}
		backend = backendVal
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
	if backendVal == "" {
		secondaryFields = append(secondaryFields, huh.NewSelect[string]().
			Title("Index backend").
			Description("How should the cache index be stored?").
			Options(
				huh.NewOption("Cloudflare R2 (recommended)", "r2"),
				huh.NewOption("Native OCI Tags (experimental)", "native"),
				huh.NewOption("JSON index stored in OCI", "oci"),
			).
			Value(&backend))
	}

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

	cfg.Backend = BackendType(backend)
	cfg.GitProvider = GitProvider(gitProvider)
	return nil
}

// promptCredentials asks for only the credentials required by the selected options.
func promptCredentials(cfg *InitConfig) error {
	// Cloudflare credentials are always required (we deploy a Worker).
	cfg.CloudflareAccountID = viper.GetString("cloudflare-account-id")
	cfg.CloudflareToken = viper.GetString("cloudflare-api-token")

	// Git token detection.
	cfg.GitToken = viper.GetString("git-token")
	if cfg.GitToken == "" {
		switch cfg.GitProvider {
		case GitGitHub:
			cfg.GitToken = detectGitHubToken()
		case GitGitLab:
			cfg.GitToken = detectGitLabToken()
		}
	}

	// Build the credentials form with only the fields that are missing.
	var fields []huh.Field

	if cfg.CloudflareAccountID == "" {
		fields = append(fields, huh.NewInput().
			Title("Cloudflare Account ID").
			Value(&cfg.CloudflareAccountID).
			Validate(notEmpty("Cloudflare Account ID")))
	}
	if cfg.CloudflareToken == "" {
		fields = append(fields, huh.NewInput().
			Title("Cloudflare API Token").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.CloudflareToken).
			Validate(notEmpty("Cloudflare API Token")))
	}

	if cfg.GitProvider == GitGitHub && cfg.GitToken == "" {
		var useOAuth bool
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("No GitHub token found. Authenticate via browser (OAuth Device Flow)?").
					Value(&useOAuth),
			),
		).WithTheme(AeroflareTheme()).Run()
		if err != nil {
			return fmt.Errorf("wizard cancelled")
		}

		if useOAuth {
			cfg.GitToken = githubDeviceFlow()
		}

		if !useOAuth || cfg.GitToken == "" {
			fields = append(fields, huh.NewInput().
				Title("GitHub Token").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.GitToken).
				Validate(notEmpty("GitHub Token")))
		}
	}

	if cfg.GitProvider == GitGitLab && cfg.GitToken == "" {
		fields = append(fields, huh.NewInput().
			Title("GitLab Token").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.GitToken).
			Validate(func(s string) error {
				if err := notEmpty("GitLab Token")(s); err != nil {
					return err
				}
				_, err := getGitLabUsername(s)
				if err != nil {
					return fmt.Errorf("invalid token: %v", err)
				}
				return nil
			}))
	}

	// Show the credentials form only if there are missing values.
	if len(fields) > 0 {
		if err := huh.NewForm(huh.NewGroup(fields...)).WithTheme(AeroflareTheme()).Run(); err != nil {
			return fmt.Errorf("wizard cancelled")
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

// DisplaySummary shows a configuration summary and asks for confirmation.
func DisplaySummary(cfg *InitConfig) (bool, error) {
	fields := []ui.BoxField{
		{Label: "Cache", Value: cfg.CacheName},
		{Label: "Registry", Value: cfg.Registry},
		{Label: "Repository", Value: cfg.Repository},
		{Label: "Backend", Value: cfg.Backend.String()},
		{Label: "Worker", Value: cfg.WorkerName},
	}
	if cfg.Backend == BackendR2 {
		fields = append(fields, ui.BoxField{Label: "R2 Bucket", Value: cfg.R2Bucket})
	}
	if cfg.GitProvider != GitNone {
		fields = append(fields, ui.BoxField{Label: "Git", Value: fmt.Sprintf("%s (%s)", cfg.GitProvider, cfg.GitUsername)})
	}

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
