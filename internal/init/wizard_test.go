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
		provider GitProvider
		initial  cmdutil.Overrides
		want     cmdutil.Overrides
	}{
		{
			name:     "cloudflare credentials come from config",
			config:   map[string]string{"cloudflare-api-token": "cf-tok", "cloudflare-account-id": "cf-acct"},
			provider: GitNone,
			want:     cmdutil.Overrides{CfToken: "cf-tok", CfUserID: "cf-acct"},
		},
		{
			name:     "git-token routes to the configured provider",
			config:   map[string]string{"git-token": "gh-tok"},
			provider: GitGitHub,
			want:     cmdutil.Overrides{GithubToken: "gh-tok"},
		},
		{
			name:     "git-token routes to gitlab when gitlab is the provider",
			config:   map[string]string{"git-token": "gl-tok"},
			provider: GitGitLab,
			want:     cmdutil.Overrides{GitlabToken: "gl-tok"},
		},
		{
			name:     "git-token is ignored without a git provider",
			config:   map[string]string{"git-token": "gh-tok"},
			provider: GitNone,
			want:     cmdutil.Overrides{},
		},
		{
			// Flags outrank the config file, so an explicitly passed --cf-token
			// or --github-token must survive seeding.
			name:     "explicit flags are not overwritten",
			config:   map[string]string{"cloudflare-api-token": "cf-cfg", "git-token": "gh-cfg"},
			provider: GitGitHub,
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
			seedOverridesFromConfig(f, tt.provider)

			if *f.Overrides != tt.want {
				t.Errorf("seedOverridesFromConfig() = %+v, want %+v", *f.Overrides, tt.want)
			}
		})
	}
}
