// Package iostreams carries the input and output streams a command reads and
// writes, so commands never reach for os.Stdout directly and can be tested by
// swapping in buffers.
package iostreams

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	// stdinTTY, when set, overrides TTY detection in tests.
	stdinTTY    *bool
	stdinIsFile *os.File
}

// System returns the IOStreams wired to the real process streams.
func System() *IOStreams {
	return &IOStreams{
		In:          os.Stdin,
		Out:         os.Stdout,
		ErrOut:      os.Stderr,
		stdinIsFile: os.Stdin,
	}
}

// Test returns an IOStreams backed by buffers, along with those buffers, for
// use in tests.
func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &IOStreams{In: in, Out: out, ErrOut: errOut}, in, out, errOut
}

// Error prints msg to stderr, prefixed with "Error: ".
func (s *IOStreams) Error(msg string) {
	_, _ = fmt.Fprintf(s.ErrOut, "Error: %s\n", msg)
}

// Success prints msg to stdout as-is. It's a distinct method from Info mainly
// so call sites read as intent ("this succeeded" vs. "just letting you know"),
// even though the formatting is currently identical.
func (s *IOStreams) Success(msg string) {
	_, _ = fmt.Fprintln(s.Out, msg)
}

// Info prints msg to stdout as-is.
func (s *IOStreams) Info(msg string) {
	_, _ = fmt.Fprintln(s.Out, msg)
}

// Warning prints msg to stdout, prefixed with "Warning: ".
func (s *IOStreams) Warning(msg string) {
	_, _ = fmt.Fprintln(s.Out, "Warning: "+msg)
}

// SetStdinTTY overrides TTY detection. Tests use it to exercise the
// interactive and non-interactive branches without a real terminal.
func (s *IOStreams) SetStdinTTY(isTTY bool) {
	s.stdinTTY = &isTTY
}

// SetStdinFile overrides the file consulted by IsStdinTTY's device check, and
// clears any override set by SetStdinTTY so the stat path is genuinely taken.
// Tests use it to exercise that path against a known file.
func (s *IOStreams) SetStdinFile(f *os.File) {
	s.stdinIsFile = f
	s.stdinTTY = nil
}

// IsStdinTTY reports whether stdin is an interactive character device, used to
// decide whether it's safe to launch an interactive prompt or whether we should
// fail with an actionable error message instead.
func (s *IOStreams) IsStdinTTY() bool {
	if s.stdinTTY != nil {
		return *s.stdinTTY
	}
	if s.stdinIsFile == nil {
		return false
	}
	stat, err := s.stdinIsFile.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
