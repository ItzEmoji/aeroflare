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

	promptWorkerToken(f, cfg)

	return cfg, nil
}

// promptWorkerToken decides what registry token to embed in the Worker as the
// NIXCACHE_TOKEN secret. An explicit worker-token (flag/config) always wins.
// Otherwise, on the ghcr.io path and only when stdin is a terminal, we prompt
// for a dedicated PAT scoped to reading the cache: keeping it separate from the
// broad token collected to push (which may be a device-flow OAuth token) means
// that account-wide credential is never embedded in a Cloudflare Worker secret.
// A blank answer — or any non-ghcr.io registry, or a non-interactive run —
// leaves the Worker authenticating anonymously, which is all a public cache
// needs. The PAT is used only for this deploy; it is not stored locally.
//
// The base64 encoding deployWorker applies to the secret is GHCR-specific: the
// Worker uses NIXCACHE_TOKEN verbatim as a bearer there, so the raw PAT is the
// value it expects.
func promptWorkerToken(f *cmdutil.Factory, cfg *InitConfig) {
	if t := viper.GetString("worker-token"); t != "" {
		cfg.WorkerToken = t
		return
	}
	if cfg.Registry != "ghcr.io" || !f.IOStreams.IsStdinTTY() {
		return
	}

	var token string
	// Optional field: a cancel/error leaves the Worker anonymous rather than
	// aborting the wizard after every other credential was already collected.
	err := huh.NewInput().
		Title("Worker registry token (optional)").
		Description("A dedicated ghcr.io PAT for the Worker, separate from your push token. Required only for private caches; leave blank for a public cache.").
		EchoMode(huh.EchoModePassword).
		Value(&token).
		WithTheme(ui.AeroflareTheme()).
		Run()
	if err != nil {
		return
	}
	cfg.WorkerToken = token
}

// promptCoreSettings asks for the cache name and registry. For each setting, a
// value already supplied via CLI flag / config (read through viper) is used
// as-is and no prompt is shown for it; only settings left unspecified are added
// to the interactive form.
func promptCoreSettings(cfg *InitConfig) error {
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
		err := huh.NewForm(huh.NewGroup(coreFields...)).WithTheme(ui.AeroflareTheme()).Run()
		if err != nil {
			return fmt.Errorf("wizard cancelled")
		}
	}

	return nil
}

// promptCredentials collects the credentials init needs: Cloudflare (always,
// to deploy the Worker) and one registry credential for cfg.Registry.
//
// Every credential is obtained through the auth module (pkg/cmd/auth/shared),
// which owns the whole chain: resolve from flag/env/secrets, prompt only for
// what's missing, and persist whatever it collects. The wizard must not grow
// its own prompting or device-flow logic — a second, non-persisting copy is
// what previously made `init` authenticate twice on a fresh machine.
func promptCredentials(f *cmdutil.Factory, cfg *InitConfig) error {
	seedOverridesFromConfig(f, cfg.Registry)

	// Cloudflare credentials are always required (we deploy a Worker).
	var err error
	cfg.CloudflareToken, cfg.CloudflareAccountID, err = shared.RequireCloudflareToken(f)
	if err != nil {
		return err
	}

	// The registry credential is keyed off the registry host: the well-known
	// registries authenticate with their provider's token, everything else with
	// a username/password pair.
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

	return nil
}

// seedOverridesFromConfig copies credentials from the config file (the keys
// `aeroflare settings` writes) into the factory's flag overrides, unless the
// matching flag was already passed. The auth module resolves overrides ahead of
// the environment and secrets manager, so this is what gives a configured
// token the priority a flag has, without the wizard reading credentials itself.
//
// The stored `git-token` key doubles as the registry login for the provider's
// own registry, so it seeds the GitHub or GitLab override based on the registry
// host rather than any (now removed) git-provider selection.
func seedOverridesFromConfig(f *cmdutil.Factory, registry string) {
	if f.Overrides.CfToken == "" {
		f.Overrides.CfToken = viper.GetString("cloudflare-api-token")
	}
	if f.Overrides.CfAccountID == "" {
		f.Overrides.CfAccountID = viper.GetString("cloudflare-account-id")
	}

	gitToken := viper.GetString("git-token")
	if gitToken == "" {
		return
	}
	switch registry {
	case "ghcr.io":
		if f.Overrides.GithubToken == "" {
			f.Overrides.GithubToken = gitToken
		}
	case "registry.gitlab.com":
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
		{Label: "Worker", Value: cfg.WorkerName},
	}
	workerToken := "none (anonymous)"
	if cfg.WorkerToken != "" {
		workerToken = "set (private/faster)"
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
	).WithTheme(ui.AeroflareTheme()).Run()
	if err != nil {
		return false, nil
	}
	return confirmed, nil
}
