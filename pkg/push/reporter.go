package push

// Reporter receives push progress events so callers control presentation. The
// push pipeline never writes to stdout itself; everything a user would see is
// routed through here.
//
// Implementations must be safe for concurrent use: uploads are reported from
// several worker goroutines at once.
//
// The CLI's terminal implementation lives in pkg/cmd/push; see NewUIReporter
// there. A caller that wants silence can implement this with empty methods.
type Reporter interface {
	// Step reports entry into stage step of total, e.g. "[2/3] Uploading...".
	Step(step, total int, msg string)
	// Uploaded reports that a store path was pushed to the registry.
	Uploaded(storePath string)
	// SkippedUpstream reports a path skipped because the upstream cache has it.
	SkippedUpstream(storePath string)
	// Success reports a completed stage.
	Success(msg string)
	// Summary reports a tally as label/value pairs.
	Summary(title string, fields [][2]string)
	// Failed reports that one store path could not be pushed, at the named
	// stage. A push continues after a per-path failure; every path reported
	// here also appears in PushResult.Failed, so a caller that only wants the
	// return value may discard these.
	Failed(storePath, stage string, err error)
	// Warn reports a non-fatal problem that is not tied to one store path.
	Warn(msg string)
	// Info reports incidental progress detail.
	Info(msg string)
}
