package aerocmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
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

			if got := handleError(f, tt.err); got != tt.wantCode {
				t.Errorf("handleError() = %d, want %d", got, tt.wantCode)
			}
			if got := errOut.String(); got != tt.wantErr {
				t.Errorf("stderr = %q, want %q", got, tt.wantErr)
			}
		})
	}
}
