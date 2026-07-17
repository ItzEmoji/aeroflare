package setup

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/spf13/viper"
)

// newOverrides builds a Factory carrying just the credential overrides, which
// is all seedOverridesFromConfig touches.
func newOverrides(o cmdutil.Overrides) *cmdutil.Factory {
	return &cmdutil.Factory{Overrides: &o}
}

func TestSeedOverridesFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]string
		registry string
		initial  cmdutil.Overrides
		want     cmdutil.Overrides
	}{
		{
			name:     "cloudflare credentials come from config",
			config:   map[string]string{"cloudflare-api-token": "cf-tok", "cloudflare-account-id": "cf-acct"},
			registry: "ghcr.io",
			want:     cmdutil.Overrides{CfToken: "cf-tok", CfUserID: "cf-acct"},
		},
		{
			name:     "git-token seeds the GitHub override for ghcr.io",
			config:   map[string]string{"git-token": "gh-tok"},
			registry: "ghcr.io",
			want:     cmdutil.Overrides{GithubToken: "gh-tok"},
		},
		{
			name:     "git-token seeds the GitLab override for registry.gitlab.com",
			config:   map[string]string{"git-token": "gl-tok"},
			registry: "registry.gitlab.com",
			want:     cmdutil.Overrides{GitlabToken: "gl-tok"},
		},
		{
			name:     "git-token is ignored for a third-party registry",
			config:   map[string]string{"git-token": "gh-tok"},
			registry: "docker.io",
			want:     cmdutil.Overrides{},
		},
		{
			// Flags outrank the config file, so an explicitly passed --cf-token
			// or --github-token must survive seeding.
			name:     "explicit flags are not overwritten",
			config:   map[string]string{"cloudflare-api-token": "cf-cfg", "git-token": "gh-cfg"},
			registry: "ghcr.io",
			initial:  cmdutil.Overrides{CfToken: "cf-flag", GithubToken: "gh-flag"},
			want:     cmdutil.Overrides{CfToken: "cf-flag", GithubToken: "gh-flag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			for k, v := range tt.config {
				viper.Set(k, v)
			}

			f := newOverrides(tt.initial)
			seedOverridesFromConfig(f, tt.registry)

			if *f.Overrides != tt.want {
				t.Errorf("seedOverridesFromConfig() = %+v, want %+v", *f.Overrides, tt.want)
			}
		})
	}
}

func TestResolveWorkerToken(t *testing.T) {
	tests := []struct {
		name        string
		workerToken string // viper "worker-token"
		registry    string
		ociToken    string
		want        string
	}{
		{
			name:     "ghcr.io reuses the collected registry PAT",
			registry: "ghcr.io",
			ociToken: "gh-pat",
			want:     "gh-pat",
		},
		{
			name:     "non-ghcr registry stays anonymous",
			registry: "registry.gitlab.com",
			ociToken: "gl-pat",
			want:     "",
		},
		{
			// An explicit worker-token wins regardless of registry, and is not
			// overwritten by the ghcr.io reuse path.
			name:        "explicit worker-token wins",
			workerToken: "explicit",
			registry:    "ghcr.io",
			ociToken:    "gh-pat",
			want:        "explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			if tt.workerToken != "" {
				viper.Set("worker-token", tt.workerToken)
			}

			cfg := &InitConfig{Registry: tt.registry, OCIToken: tt.ociToken}
			resolveWorkerToken(cfg)

			if cfg.WorkerToken != tt.want {
				t.Errorf("resolveWorkerToken() WorkerToken = %q, want %q", cfg.WorkerToken, tt.want)
			}
		})
	}
}
