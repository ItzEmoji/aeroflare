package run

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
)

// The invariant that used to be enforced by run.go and push.go binding to the
// same package-level vars: both commands must expose the same push flags.
// The matching assertion for push lives in pkg/cmd/push (this test cannot
// live there since run imports push, which would be an import cycle).
func TestRunExposesTheSameSharedFlagsAsPush(t *testing.T) {
	io, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: io, Overrides: &cmdutil.Overrides{}}

	runCmd := NewCmdRun(f)
	shared := []string{"compression", "upstream-cache", "workers", "prepare-refs", "signing-key", "keep", "force"}
	for _, name := range shared {
		if runCmd.Flags().Lookup(name) == nil {
			t.Errorf("run is missing shared flag --%s", name)
		}
	}
}
