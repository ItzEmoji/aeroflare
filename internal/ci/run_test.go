package ci

import (
	"strings"
	"testing"
)

func TestSummaryLine_Success(t *testing.T) {
	got := summaryLine(2, 2, 1, 1, 417)
	if !strings.Contains(got, "builds 2/2") || !strings.Contains(got, "pushes 1/1") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "paths 417") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "OK") {
		t.Errorf("expected OK marker, got %q", got)
	}
}

func TestSummaryLine_Failure(t *testing.T) {
	got := summaryLine(2, 1, 1, 0, 5)
	if !strings.Contains(got, "FAILED") {
		t.Errorf("expected FAILED marker, got %q", got)
	}
}

func TestNothingToPushLine(t *testing.T) {
	got := nothingToPushLine(12)
	if !strings.Contains(got, "12") {
		t.Errorf("expected the path count, got %q", got)
	}
	if !strings.Contains(got, "already upstream") {
		t.Errorf("expected an already-upstream note, got %q", got)
	}
}

// Every root already upstream is a success, not a failure: there is genuinely
// nothing to do.
func TestSummaryLine_NoPushesIsStillOK(t *testing.T) {
	got := summaryLine(2, 2, 0, 0, 0)
	if !strings.Contains(got, "OK") {
		t.Errorf("expected OK, got %q", got)
	}
}

func TestPrepareScope_NoUpstreamsIsFullClosure(t *testing.T) {
	got := prepareScope(nil)
	if got != "full closure" {
		t.Errorf("prepareScope(nil) = %q, want %q", got, "full closure")
	}
}

func TestPrepareScope_WithUpstreamsIsFiltered(t *testing.T) {
	got := prepareScope([]string{"https://cache.nixos.org"})
	if got == "full closure" {
		t.Fatalf("with an upstream configured the set is not the full closure, got %q", got)
	}
	if !strings.Contains(got, "upstream") {
		t.Errorf("expected the scope to mention upstream, got %q", got)
	}
}
