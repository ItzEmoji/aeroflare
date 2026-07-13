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
			wantPort:      37515,
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
			wantPort:      37515,
			wantListen:    "127.0.0.1",
			wantUpstreams: []string{"https://a.example.com", "https://b.example.com"},
		},
		{
			name:          "a malformed port falls back to the default rather than crashing",
			port:          "not-a-number",
			wantPort:      37515,
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
