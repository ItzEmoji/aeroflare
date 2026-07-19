package push

import (
	"fmt"
	"path/filepath"

	"github.com/itzemoji/aeroflare/internal/ui"
	internalpush "github.com/itzemoji/aeroflare/pkg/push"

	"github.com/charmbracelet/lipgloss"
)

// NewUIReporter returns a push.Reporter that renders progress with the
// charm/lipgloss UI, matching the aeroflare CLI's output. verbose mirrors the
// Verbosity>=1 behavior.
//
// Terminal rendering lives here, in the command layer, rather than in pkg/push:
// a library must not write to its caller's stdout.
func NewUIReporter(verbose bool) internalpush.Reporter { return uiReporter{verbose: verbose} }

type uiReporter struct{ verbose bool }

// Compile-time assertion that uiReporter satisfies the interface.
var _ internalpush.Reporter = uiReporter{}

func (u uiReporter) Step(step, total int, msg string) { printStep(step, total, msg) }

func (u uiReporter) Uploaded(storePath string) { printSuccess(filepath.Base(storePath)) }

func (u uiReporter) SkippedUpstream(storePath string) {
	fmt.Printf("Skipping %s (already in upstream cache)\n", storePath)
}

func (u uiReporter) Success(msg string) { printSuccess(msg) }

func (u uiReporter) Failed(storePath, stage string, err error) {
	fmt.Printf("ERROR: %s (%s): %v\n", stage, storePath, err)
}

func (u uiReporter) Warn(msg string) { fmt.Printf("WARNING: %s\n", msg) }

func (u uiReporter) Info(msg string) { fmt.Println(msg) }

func (u uiReporter) Summary(title string, fields [][2]string) {
	bf := make([]ui.BoxField, len(fields))
	for i, f := range fields {
		bf[i] = ui.BoxField{Label: f[0], Value: f[1]}
	}
	ui.PrintSummaryBox(title, bf)
}

// printStep prints a "[step/total] msg" progress line, e.g. "[2/3] Uploading...".
func printStep(step, total int, msg string) {
	fmt.Printf("\n  [%d/%d] %s\n", step, total, msg)
}

// printSuccess prints msg prefixed with a green checkmark.
func printSuccess(msg string) {
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("✓")
	fmt.Printf("  %s %s\n", checkMark, msg)
}
