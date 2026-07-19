package aerocmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/spf13/cobra"
)

func TestHandleError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode exitCode
		wantErr  string
	}{
		{name: "plain error is reported and exits 1", err: errors.New("boom"), wantCode: exitError, wantErr: "Error: boom\n"},
		{name: "cancel exits 2 silently", err: cmdutil.ErrCancel, wantCode: exitCancel, wantErr: ""},
		{name: "silent exits 1 without printing", err: cmdutil.ErrSilent, wantCode: exitError, wantErr: ""},
		{name: "wrapped cancel is still a cancel", err: fmt.Errorf("form: %w", cmdutil.ErrCancel), wantCode: exitCancel, wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _, errOut := iostreams.Test()
			f := &cmdutil.Factory{IOStreams: io}

			if got := handleError(f, nil, tt.err); got != tt.wantCode {
				t.Errorf("handleError() = %d, want %d", got, tt.wantCode)
			}
			if got := errOut.String(); got != tt.wantErr {
				t.Errorf("stderr = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

// TestHandleErrorFlagErrorPrintsUsage is a regression test for usage being
// silently dropped on flag/argument errors after SilenceUsage was
// introduced. A FlagError must print the failing command's usage in
// addition to the error message; a plain runtime error (tested above) must
// not print any usage block.
func TestHandleErrorFlagErrorPrintsUsage(t *testing.T) {
	io, _, _, errOut := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: io}

	cmd := &cobra.Command{
		Use: "push [flags]",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}
	cmd.Flags().String("cache-url", "", "OCI registry URL for the cache")

	err := cmdutil.FlagErrorf("unknown flag: --bogus")

	if got := handleError(f, cmd, err); got != exitError {
		t.Errorf("handleError() = %d, want %d", got, exitError)
	}

	out := errOut.String()
	if !strings.Contains(out, "Error: unknown flag: --bogus") {
		t.Errorf("stderr = %q, want it to contain the error message", out)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "push [flags]") {
		t.Errorf("stderr = %q, want it to contain push's usage block", out)
	}
}
