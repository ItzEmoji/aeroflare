package cmdutil_test

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/spf13/viper"
)

// The proxy and push packages no longer read oci_token / NIXCACHE_TOKEN
// themselves; this is the CLI-side lookup that feeds them.
func TestRegistryOverrideToken(t *testing.T) {
	tests := []struct {
		name        string
		ociToken    string
		nixcacheTok string
		want        string
	}{
		{
			name:     "oci_token is used verbatim",
			ociToken: "direct-oci-token-value",
			want:     "direct-oci-token-value",
		},
		{
			name:        "NIXCACHE_TOKEN is the fallback",
			nixcacheTok: "nixcache-direct-token",
			want:        "nixcache-direct-token",
		},
		{
			name:        "oci_token wins over NIXCACHE_TOKEN",
			ociToken:    "first",
			nixcacheTok: "second",
			want:        "first",
		},
		{
			// A raw PAT is not a bearer token: fall through to exchange
			// rather than send an invalid Authorization header.
			name:     "a raw PAT is rejected",
			ociToken: "ghp_mypersonalaccesstoken",
			want:     "",
		},
		{
			name: "nothing set",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("oci_token", tt.ociToken)
			t.Setenv("NIXCACHE_TOKEN", tt.nixcacheTok)

			if got := cmdutil.RegistryOverrideToken(); got != tt.want {
				t.Errorf("RegistryOverrideToken() = %q, want %q", got, tt.want)
			}
		})
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
