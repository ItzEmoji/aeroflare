package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
	"github.com/spf13/viper"
)

func TestResolveCacheURL(t *testing.T) {
	tests := []struct {
		name     string
		cacheURL string
		cache    string
		expected string
	}{
		{name: "both empty", cacheURL: "", cache: "", expected: ""},
		{name: "cache-url wins", cacheURL: "oci://example.com/foo", cache: "org/repo", expected: "oci://example.com/foo"},
		{name: "cache shorthand expands to ghcr.io", cacheURL: "", cache: "org/repo", expected: "ghcr.io/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.Set("cache-url", tt.cacheURL)
			v.Set("cache", tt.cache)

			if got := ResolveCacheURL(v); got != tt.expected {
				t.Errorf("ResolveCacheURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestCacheURLFlagOverridesConfigOnSubcommand is a regression test for a bug
// where the --cache-url persistent flag was silently ignored in favor of the
// config file value. The viper bind happens in PersistentPreRunE, but cobra
// invokes an inherited PersistentPreRunE with `cmd` bound to the
// actually-executed leaf command, not the root command. Binding against the
// leaf's (empty) persistent flag set looked up a nil flag and the resulting
// error was discarded, so the bind never happened. This must be exercised
// through an executed subcommand (e.g. `version`) to reproduce: building or
// executing the root command alone does not trigger the leaf-vs-root
// shadowing.
func TestCacheURLFlagOverridesConfigOnSubcommand(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	// Use a real config file rather than v.Set, since v.Set installs an
	// explicit override that outranks even a bound flag in viper's
	// precedence order. A config file sits below a bound flag but above a
	// default, which is what we need to exercise the actual bug: does the
	// flag win over the config file the way it does in production?
	configFile := filepath.Join(t.TempDir(), "aeroflare.yaml")
	if err := os.WriteFile(configFile, []byte("cache-url: oci://from-config\n"), 0644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	v := viper.New()
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig() = %v", err)
	}
	f.Config = func() (*viper.Viper, error) { return v, nil }

	cmd := NewCmdRoot(f, "test", "")
	cmd.SetArgs([]string{"version", "--cache-url", "oci://from-flag"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if got := ResolveCacheURL(v); got != "oci://from-flag" {
		t.Errorf("ResolveCacheURL() after Execute = %q, want %q (--cache-url flag should override config file)", got, "oci://from-flag")
	}
}
