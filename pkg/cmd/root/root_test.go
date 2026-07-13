package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
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

// TestInitConfigUsesGlobalViper is a regression test for a bug where
// InitConfig() created a private viper.New() instance instead of binding
// the global viper.GetViper() singleton. pkg/oci and internal/init read
// config via package-level viper.GetString calls (i.e. the global
// singleton), so a private instance meant --cache-url, AEROFLARE_CACHE, and
// the config file were silently ignored everywhere except NIXCACHE_REGISTRY
// /NIXCACHE_REPO. This must be exercised through an executed command (not a
// direct InitConfig() call) and through pkg/cmdutil.RegistryAndRepository,
// the same path production code takes, to actually catch the regression.
func TestInitConfigUsesGlobalViper(t *testing.T) {
	t.Run("cache-url flag", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		f, _, _ := cmdutiltest.NewTestFactory(t, nil)
		f.Config = initConfigAdapter

		cmd := NewCmdRoot(f, "test", "")
		cmd.SetArgs([]string{"version", "--cache-url", "oci://ghcr.io/foo/bar"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() = %v, want nil", err)
		}

		registry, repository, err := cmdutil.RegistryAndRepository()
		if err != nil {
			t.Fatalf("GetRegistryAndRepository() error = %v, want nil", err)
		}
		if registry != "ghcr.io" || repository != "foo/bar" {
			t.Errorf("GetRegistryAndRepository() = (%q, %q), want (%q, %q)", registry, repository, "ghcr.io", "foo/bar")
		}
	})

	t.Run("AEROFLARE_CACHE env var", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("AEROFLARE_CACHE", "foo/bar")

		f, _, _ := cmdutiltest.NewTestFactory(t, nil)
		f.Config = initConfigAdapter

		cmd := NewCmdRoot(f, "test", "")
		cmd.SetArgs([]string{"version"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() = %v, want nil", err)
		}

		registry, repository, err := cmdutil.RegistryAndRepository()
		if err != nil {
			t.Fatalf("GetRegistryAndRepository() error = %v, want nil", err)
		}
		if registry != "ghcr.io" || repository != "foo/bar" {
			t.Errorf("GetRegistryAndRepository() = (%q, %q), want (%q, %q)", registry, repository, "ghcr.io", "foo/bar")
		}
	})
}

// initConfigAdapter adapts InitConfig's (v, isNew, err) return to the
// cmdutil.Factory.Config shape of (v, err), for use as f.Config in tests.
func initConfigAdapter() (*viper.Viper, error) {
	v, _, err := InitConfig()
	return v, err
}
