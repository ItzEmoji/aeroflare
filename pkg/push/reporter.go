package push

import (
	"fmt"
	"path/filepath"

	"github.com/itzemoji/aeroflare/internal/ui"
)

// Reporter receives push progress events so callers control presentation.
type Reporter interface {
	Step(step, total int, msg string)
	Uploaded(storePath string)
	SkippedUpstream(storePath string)
	Success(msg string)
	Summary(title string, fields [][2]string)
}

// uiReporter renders progress with the existing charm/lipgloss UI, matching the
// legacy aeroflare CLI output. verbose mirrors the old Verbosity>=1 behavior.
type uiReporter struct{ verbose bool }

func (u uiReporter) Step(step, total int, msg string) { printStep(step, total, msg) }

func (u uiReporter) Uploaded(storePath string) { printSuccess(filepath.Base(storePath)) }

func (u uiReporter) SkippedUpstream(storePath string) {
	fmt.Printf("Skipping %s (already in upstream cache)\n", storePath)
}

func (u uiReporter) Success(msg string) { printSuccess(msg) }

func (u uiReporter) Summary(title string, fields [][2]string) {
	bf := make([]ui.BoxField, len(fields))
	for i, f := range fields {
		bf[i] = ui.BoxField{Label: f[0], Value: f[1]}
	}
	ui.PrintSummaryBox(title, bf)
}
