package cmdutil_test

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/spf13/viper"
)

// A credential resolved from the environment is a password: the registry is
// what turns it into a bearer token. Presenting it as a bearer instead is what
// broke pushes from GitHub Actions, whose GITHUB_TOKEN is a "ghs_" PAT.
func TestRegistryAuth_ResolvesCredentialsAsPasswords(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		env      map[string]string
		want     string
	}{
		{
			name:     "a GitHub Actions token is a password, not a bearer",
			registry: "ghcr.io",
			env:      map[string]string{"GITHUB_TOKEN": "ghs_actionsToken"},
			want:     "ghs_actionsToken",
		},
		{
			name:     "a classic PAT is a password too",
			registry: "ghcr.io",
			env:      map[string]string{"GITHUB_TOKEN": "ghp_classicToken"},
			want:     "ghp_classicToken",
		},
		{
			name:     "oci_token supplies a generic registry's password",
			registry: "harbor.example.com",
			env:      map[string]string{"oci_token": "some-harbor-password"},
			want:     "some-harbor-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			auth := cmdutil.RegistryAuth(tt.registry, "")
			if auth == nil {
				t.Fatal("RegistryAuth returned nil; expected a credential from the environment")
			}
			cfg, err := auth.Authorization()
			if err != nil {
				t.Fatal(err)
			}

			if cfg.Password != tt.want {
				t.Errorf("Password = %q, want %q", cfg.Password, tt.want)
			}
			if cfg.RegistryToken != "" {
				t.Errorf("RegistryToken = %q; the credential must be exchanged by the registry, not sent as a bearer", cfg.RegistryToken)
			}
		})
	}
}

// An explicit token (a --token flag, or the git token init already collected)
// takes precedence, and is likewise a password.
func TestRegistryAuth_ExplicitTokenWins(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-the-environment")

	auth := cmdutil.RegistryAuth("ghcr.io", "explicitly-passed")
	if auth == nil {
		t.Fatal("RegistryAuth returned nil")
	}
	cfg, err := auth.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "explicitly-passed" {
		t.Errorf("Password = %q, want the explicit token", cfg.Password)
	}
}

func TestRegistryAndRepository(t *testing.T) {
	tests := []struct {
		name     string
		cacheURL string
		cache    string
		registry string
		envReg   string
		envRepo  string
		wantReg  string
		wantRepo string
		wantErr  bool
	}{
		{
			name:     "cache-url with host splits into registry and repository",
			cacheURL: "oci://ghcr.io/foo/bar",
			wantReg:  "ghcr.io",
			wantRepo: "foo/bar",
		},
		{
			name:     "cache shorthand falls back to ghcr.io and lowercases",
			cache:    "Foo/Bar",
			wantReg:  "ghcr.io",
			wantRepo: "foo/bar",
		},
		{
			name:     "explicit registry key is honored",
			registry: "example.com",
			cache:    "foo/bar",
			wantReg:  "example.com",
			wantRepo: "foo/bar",
		},
		{
			name:     "NIXCACHE_* env vars are the fallback when viper is unset",
			envReg:   "docker.io",
			envRepo:  "MyCache",
			wantReg:  "docker.io",
			wantRepo: "mycache",
		},
		{
			// The case that used to call os.Exit(1) and kill the process.
			name:    "no cache configured returns an error instead of exiting",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			// NIXCACHE_* env vars are a fallback path inside the function;
			// clear them so they can't leak in from the developer's shell.
			t.Setenv("NIXCACHE_REGISTRY", tt.envReg)
			t.Setenv("NIXCACHE_REPO", tt.envRepo)

			if tt.cacheURL != "" {
				viper.Set("cache-url", tt.cacheURL)
			}
			if tt.cache != "" {
				viper.Set("cache", tt.cache)
			}
			if tt.registry != "" {
				viper.Set("registry", tt.registry)
			}

			reg, repo, err := cmdutil.RegistryAndRepository()

			if tt.wantErr {
				if err == nil {
					t.Fatal("RegistryAndRepository() = nil error, want an error when no cache is configured")
				}
				if !strings.Contains(err.Error(), "AEROFLARE_CACHE") {
					t.Errorf("error %q should name the AEROFLARE_CACHE config the user must set", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("RegistryAndRepository() error = %v, want nil", err)
			}
			if reg != tt.wantReg {
				t.Errorf("registry = %q, want %q", reg, tt.wantReg)
			}
			if repo != tt.wantRepo {
				t.Errorf("repository = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}
