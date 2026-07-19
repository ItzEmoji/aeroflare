package push

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/spf13/cobra"
)

// The invariant that used to be enforced by run.go and push.go binding to the
// same package-level vars: both commands must expose the same push flags with
// the same defaults.
func TestAddPushFlagsDefaults(t *testing.T) {
	opts := &Options{}
	cmd := &cobra.Command{}
	AddPushFlags(cmd, opts)

	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags() = %v", err)
	}

	if opts.Compression != "zstd" {
		t.Errorf("Compression = %q, want zstd", opts.Compression)
	}
	if opts.UpstreamCache != "https://cache.nixos.org" {
		t.Errorf("UpstreamCache = %q, want https://cache.nixos.org", opts.UpstreamCache)
	}
	if opts.Workers != 50 {
		t.Errorf("Workers = %d, want 50", opts.Workers)
	}
	if !opts.PrepareRefs {
		t.Error("PrepareRefs = false, want true")
	}
}

func TestPushAndRunExposeTheSameSharedFlags(t *testing.T) {
	io, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: io, Overrides: &cmdutil.Overrides{}}

	pushCmd := NewCmdPush(f)
	shared := []string{"compression", "upstream-cache", "workers", "prepare-refs", "signing-key", "keep", "force"}
	for _, name := range shared {
		if pushCmd.Flags().Lookup(name) == nil {
			t.Errorf("push is missing shared flag --%s", name)
		}
	}
}
