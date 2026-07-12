package cmdutil

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// ErrSilent signals that a command has already reported its failure to the
// user and aerocmd.Main should exit non-zero without printing anything more.
var ErrSilent = errors.New("SilentError")

// ErrCancel signals that the user deliberately aborted an interactive prompt.
// aerocmd.Main exits 2 without printing an error: a cancellation is not a
// failure, and the old behavior of reporting it as one was misleading.
var ErrCancel = errors.New("CancelError")

// FlagError signals that a command failed because of bad flags or arguments,
// rather than a runtime failure. aerocmd.Main treats it specially: in
// addition to the error message, it prints the failing command's usage,
// matching cobra's pre-refactor default behavior (which this repo's root
// command otherwise suppresses via SilenceUsage).
type FlagError struct {
	err error
}

func (fe *FlagError) Error() string {
	return fe.err.Error()
}

func (fe *FlagError) Unwrap() error {
	return fe.err
}

// FlagErrorf formats and wraps an error as a *FlagError.
func FlagErrorf(format string, args ...any) error {
	return FlagErrorWrap(fmt.Errorf(format, args...))
}

// FlagErrorWrap wraps err as a *FlagError.
func FlagErrorWrap(err error) error {
	return &FlagError{err: err}
}

// FlagErrorArgs wraps a cobra.PositionalArgs validator so any error it
// returns is a *FlagError. Argument-count errors (e.g. cobra.MinimumNArgs,
// cobra.ExactArgs) are returned from the Args validator rather than from
// flag parsing, so they need to be wrapped explicitly for aerocmd.Main to
// print usage for them the same way it does for flag-parse errors.
func FlagErrorArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return FlagErrorWrap(err)
		}
		return nil
	}
}
