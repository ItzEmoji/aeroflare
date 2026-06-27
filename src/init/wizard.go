package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
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

	cfg.Registry = "ghcr.io"

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Cache name").
				Description("A unique name for your binary cache (e.g. myuser/my-cache)").
				Value(&cfg.CacheName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("cache name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("OCI registry").
				Description("Container registry for storing cache data").
				Value(&cfg.Registry),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Index backend").
				Description("How should the cache index be stored?").
				Options(
					huh.NewOption("Cloudflare R2 (recommended)", "r2"),
					huh.NewOption("JSON index stored in OCI", "oci"),
				).
				Value(&backend),
			huh.NewSelect[string]().
				Title("Git integration").
				Description("Connect a Git repository for automatic CI/CD deployments?").
				Options(
					huh.NewOption("None", "none"),
					huh.NewOption("GitHub", "github"),
					huh.NewOption("GitLab", "gitlab"),
				).
				Value(&gitProvider),
		),
	).WithTheme(AeroflareTheme()).Run()
	if err != nil {
		return fmt.Errorf("wizard cancelled")
	}

	cfg.Backend = BackendType(backend)
	cfg.GitProvider = GitProvider(gitProvider)
	return nil
}

// promptCredentials asks for only the credentials required by the selected options.
func promptCredentials(cfg *InitConfig) error {
	// Cloudflare credentials are always required (we deploy a Worker).
	cfg.CloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	cfg.CloudflareToken = os.Getenv("CLOUDFLARE_API_TOKEN")

	// Git token detection.
	switch cfg.GitProvider {
	case GitGitHub:
		cfg.GitToken = detectGitHubToken()
	case GitGitLab:
		cfg.GitToken = detectGitLabToken()
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
		} else {
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
			Validate(notEmpty("GitLab Token")))
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
	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────────────╮")
	fmt.Println("  │  Summary                                     │")
	fmt.Println("  ├──────────────────────────────────────────────┤")
	printField("Cache", cfg.CacheName)
	printField("Registry", cfg.Registry)
	printField("Repository", cfg.Repository)
	printField("Backend", cfg.Backend.String())
	printField("Worker", cfg.WorkerName)
	if cfg.Backend == BackendR2 {
		printField("R2 Bucket", cfg.R2Bucket)
	}
	if cfg.GitProvider != GitNone {
		printField("Git", fmt.Sprintf("%s (%s)", cfg.GitProvider, cfg.GitUsername))
	}
	fmt.Println("  ╰──────────────────────────────────────────────╯")
	fmt.Println()

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

func printField(label, value string) {
	fmt.Printf("  \u2502  %-12s %s\n", label+":", value)
}

func notEmpty(name string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}
