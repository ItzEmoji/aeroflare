package configure

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
	"github.com/spf13/viper"
)

// configure resolves its target before it prompts or touches the network, so
// with no cache configured it must fail with an actionable error rather than
// opening a form for a cache that does not exist.
func TestConfigureErrorsWithoutCacheConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("NIXCACHE_REGISTRY", "")
	t.Setenv("NIXCACHE_REPO", "")

	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdConfigure(f)
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
