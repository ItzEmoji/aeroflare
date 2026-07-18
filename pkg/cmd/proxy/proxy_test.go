package proxy

import (
	"slices"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
	"github.com/spf13/viper"
)

func TestProxySettingsFromEnv(t *testing.T) {
	tests := []struct {
		name          string
		port          string
		listen        string
		upstream      string
		wantPort      int
		wantListen    string
		wantUpstreams []string
	}{
		{
			name:          "unset falls back to the defaults",
			wantPort:      8080,
			wantListen:    "127.0.0.1",
			wantUpstreams: []string{"https://cache.nixos.org"},
		},
		{
			name:          "each var is honored when set",
			port:          "8080",
			listen:        "0.0.0.0",
			upstream:      "https://example.com",
			wantPort:      8080,
			wantListen:    "0.0.0.0",
			wantUpstreams: []string{"https://example.com"},
		},
		{
			name:          "upstreams are whitespace-split into a list",
			upstream:      "https://a.example.com  https://b.example.com",
			wantPort:      8080,
			wantListen:    "127.0.0.1",
			wantUpstreams: []string{"https://a.example.com", "https://b.example.com"},
		},
		{
			name:          "a malformed port falls back to the default rather than crashing",
			port:          "not-a-number",
			wantPort:      8080,
			wantListen:    "127.0.0.1",
			wantUpstreams: []string{"https://cache.nixos.org"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NIXCACHE_PORT", tt.port)
			t.Setenv("NIXCACHE_LISTEN", tt.listen)
			t.Setenv("NIXCACHE_UPSTREAM", tt.upstream)

			port, listen, upstreams := proxySettingsFromEnv()

			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
			if listen != tt.wantListen {
				t.Errorf("listen = %q, want %q", listen, tt.wantListen)
			}
			if !slices.Equal(upstreams, tt.wantUpstreams) {
				t.Errorf("upstreams = %q, want %q", upstreams, tt.wantUpstreams)
			}
		})
	}
}

func TestProxyDisplayHost(t *testing.T) {
	tests := []struct {
		listenAddr string
		want       string
	}{
		{"127.0.0.1", "127.0.0.1"},
		{"192.168.1.10", "192.168.1.10"},
		{"", "127.0.0.1"},
		{"0.0.0.0", "127.0.0.1"},
		{"::", "127.0.0.1"},
		{"[::]", "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.listenAddr, func(t *testing.T) {
			if got := proxyDisplayHost(tt.listenAddr); got != tt.want {
				t.Errorf("proxyDisplayHost(%q) = %q, want %q", tt.listenAddr, got, tt.want)
			}
		})
	}
}

func TestResolveProxyToken(t *testing.T) {
	tests := []struct {
		name     string
		flag     string // --token
		env      string // NIXCACHE_TOKEN
		stored   map[string]string
		registry string
		want     string
	}{
		{
			name:     "the --token flag wins over env and the saved credential",
			flag:     "flag-tok",
			env:      "env-tok",
			stored:   map[string]string{"github-token": "saved-tok"},
			registry: "ghcr.io",
			want:     "flag-tok",
		},
		{
			name:     "NIXCACHE_TOKEN is used when no flag is given",
			env:      "env-tok",
			stored:   map[string]string{"github-token": "saved-tok"},
			registry: "ghcr.io",
			want:     "env-tok",
		},
		{
			name:     "falls back to the saved registry credential",
			stored:   map[string]string{"github-token": "saved-tok"},
			registry: "ghcr.io",
			want:     "saved-tok",
		},
		{
			name:     "empty when nothing is specified or saved",
			registry: "ghcr.io",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			// Isolate from the ambient environment so the saved-credential and
			// empty cases don't pick up a real GITHUB_TOKEN/oci_token.
			t.Setenv("NIXCACHE_TOKEN", tt.env)
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv("GH_TOKEN", "")
			t.Setenv("oci_token", "")

			f, _, _ := cmdutiltest.NewTestFactory(t, tt.stored)
			opts := &Options{IO: f.IOStreams, Token: tt.flag}

			got := resolveProxyToken(f, opts, tt.registry)
			if got != tt.want {
				t.Errorf("resolveProxyToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

// proxy resolves its target before it binds a port, so a missing cache config
// must surface as an actionable error rather than a listener on a bogus target.
func TestProxyErrorsWithoutCacheConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("NIXCACHE_REGISTRY", "")
	t.Setenv("NIXCACHE_REPO", "")

	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdProxy(f)
	cmd.SetArgs(nil)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error when no cache is configured")
	}
	if !strings.Contains(err.Error(), "AEROFLARE_CACHE") {
		t.Errorf("error %q should tell the user which config to set", err)
	}
}
