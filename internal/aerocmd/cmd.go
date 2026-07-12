// Package aerocmd is the aeroflare entrypoint: it builds the Factory, runs the
// root command, and translates the result into a process exit code. It lives
// under internal/ because an entrypoint is not API.
package aerocmd

import (
	"errors"
	"fmt"

	"github.com/itzemoji/aeroflare/internal/build"
	"github.com/itzemoji/aeroflare/pkg/cmd/root"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
)

type exitCode int

const (
	exitOK     exitCode = 0
	exitError  exitCode = 1
	exitCancel exitCode = 2
)

func Main() exitCode {
	f := NewFactory(build.Version)

	rootCmd := root.NewCmdRoot(f, build.Version, build.Date)
	if _, err := rootCmd.ExecuteC(); err != nil {
		return handleError(f, err)
	}
	return exitOK
}

// handleError maps a command's returned error to an exit code, printing it
// exactly once. Commands never print their own failures and never call
// os.Exit; that is this function's job.
func handleError(f *cmdutil.Factory, err error) exitCode {
	if errors.Is(err, cmdutil.ErrCancel) {
		// A deliberate abort is not a failure. Say nothing.
		return exitCancel
	}
	if errors.Is(err, cmdutil.ErrSilent) {
		// Already reported by the command.
		return exitError
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "Error: %s\n", err.Error())
	return exitError
}
