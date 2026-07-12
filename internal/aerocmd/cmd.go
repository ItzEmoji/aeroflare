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
	"github.com/spf13/cobra"
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
	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		return handleError(f, cmd, err)
	}
	return exitOK
}

// handleError maps a command's returned error to an exit code, printing it
// exactly once. Commands never print their own failures and never call
// os.Exit; that is this function's job.
//
// cmd is the command cobra identified as the target of execution (returned
// by ExecuteC), which may differ from the root command -- e.g. for
// `aeroflare push --bogus`, cmd is push, not root. It is used to print that
// command's usage when err is a cmdutil.FlagError (bad flags or arguments),
// matching the pre-refactor behavior of showing usage for those errors while
// keeping genuine runtime failures free of a trailing usage block.
func handleError(f *cmdutil.Factory, cmd *cobra.Command, err error) exitCode {
	if errors.Is(err, cmdutil.ErrCancel) {
		// A deliberate abort is not a failure. Say nothing.
		return exitCancel
	}
	if errors.Is(err, cmdutil.ErrSilent) {
		// Already reported by the command.
		return exitError
	}
	_, _ = fmt.Fprintf(f.IOStreams.ErrOut, "Error: %s\n", err.Error())

	var flagErr *cmdutil.FlagError
	if errors.As(err, &flagErr) && cmd != nil {
		_, _ = fmt.Fprintln(f.IOStreams.ErrOut)
		_, _ = fmt.Fprint(f.IOStreams.ErrOut, cmd.UsageString())
	}

	return exitError
}
