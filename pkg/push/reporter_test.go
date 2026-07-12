package push

import "testing"

// Compile-time assertion that uiReporter satisfies Reporter.
var _ Reporter = uiReporter{}

func TestUIReporterImplementsReporter(t *testing.T) {
	// The compile-time assertion above is the real check; this test documents
	// intent and fails to build if the interface drifts.
}
