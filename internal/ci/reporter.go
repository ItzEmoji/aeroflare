package ci

import (
	"fmt"
	"io"
	"strings"
)

// PlainReporter implements push.Reporter with plain, CI-friendly output.
// prefix indents every line so per-cache push output nests under its header.
type PlainReporter struct {
	w      io.Writer
	prefix string
}

// NewPlainReporter returns a PlainReporter writing to w with the given indent.
func NewPlainReporter(w io.Writer, prefix string) *PlainReporter {
	return &PlainReporter{w: w, prefix: prefix}
}

func (r *PlainReporter) Step(step, total int, msg string) {
	_, _ = fmt.Fprintf(r.w, "%s[%d/%d] %s\n", r.prefix, step, total, msg)
}

func (r *PlainReporter) Uploaded(storePath string) {
	_, _ = fmt.Fprintf(r.w, "%s  ✓ uploaded  %s\n", r.prefix, storePath)
}

func (r *PlainReporter) SkippedUpstream(storePath string) {
	_, _ = fmt.Fprintf(r.w, "%s  - skipped   %s  (already upstream)\n", r.prefix, storePath)
}

func (r *PlainReporter) Success(msg string) {
	_, _ = fmt.Fprintf(r.w, "%s  %s\n", r.prefix, msg)
}

func (r *PlainReporter) Summary(title string, fields [][2]string) {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%s %s", f[0], f[1]))
	}
	_, _ = fmt.Fprintf(r.w, "%s  (%s)\n", r.prefix, strings.Join(parts, ", "))
}

// Failed, Warn, and Info previously escaped this reporter: the push engine
// printed them straight to stdout, so they bypassed r.w and lost the indent.
func (r *PlainReporter) Failed(storePath, stage string, err error) {
	_, _ = fmt.Fprintf(r.w, "%s  ✗ failed    %s  (%s: %v)\n", r.prefix, storePath, stage, err)
}

func (r *PlainReporter) Warn(msg string) {
	_, _ = fmt.Fprintf(r.w, "%s  ! %s\n", r.prefix, msg)
}

func (r *PlainReporter) Info(msg string) {
	_, _ = fmt.Fprintf(r.w, "%s  %s\n", r.prefix, strings.TrimSpace(msg))
}
