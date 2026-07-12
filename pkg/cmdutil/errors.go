package cmdutil

import "errors"

// ErrSilent signals that a command has already reported its failure to the
// user and aerocmd.Main should exit non-zero without printing anything more.
var ErrSilent = errors.New("SilentError")

// ErrCancel signals that the user deliberately aborted an interactive prompt.
// aerocmd.Main exits 2 without printing an error: a cancellation is not a
// failure, and the old behavior of reporting it as one was misleading.
var ErrCancel = errors.New("CancelError")
