package prepare

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

// prepare needs something to prepare: with neither --store-path nor --input it
// must say so rather than silently doing nothing.
func TestPrepareRequiresStorePathOrInput(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdPrepare(f)
	cmd.SetArgs(nil)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() = nil, want an error when neither --store-path nor --input is given")
	}
	if got, want := err.Error(), "--store-path or --input is required"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// An unknown --compression must be rejected before any work starts.
func TestPrepareRejectsUnknownCompression(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	cmd := NewCmdPrepare(f)
	cmd.SetArgs([]string{"--store-path", "/nix/store/abc-def", "--compression", "brotli"})
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() = nil, want an error for an unsupported compression type")
	}
}
