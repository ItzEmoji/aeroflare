package blob

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
	"github.com/spf13/viper"
)

// clearCacheConfig removes every source GetRegistryAndRepository consults, so
// the commands below genuinely run with no cache configured.
func clearCacheConfig(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("NIXCACHE_REGISTRY", "")
	t.Setenv("NIXCACHE_REPO", "")
}

// With no cache configured, push-blob must fail with an actionable error --
// not exit the process, and not proceed to a nonsense registry.
func TestPushBlobErrorsWithoutCacheConfig(t *testing.T) {
	clearCacheConfig(t)

	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdPushBlob(f)
	cmd.SetArgs([]string{"/some/file"})
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

func TestPullBlobErrorsWithoutCacheConfig(t *testing.T) {
	clearCacheConfig(t)

	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdPullBlob(f)
	cmd.SetArgs([]string{"sha256:deadbeef", "/some/out"})
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
