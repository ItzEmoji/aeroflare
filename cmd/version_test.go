package cmd

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/internal/build"
)

func TestVersionCmdPrintsBuildVersion(t *testing.T) {
	original := build.Version
	build.Version = "v1.2.3-test"
	defer func() { build.Version = original }()

	output, err := executeCommand(rootCmd, "version")
	if err != nil {
		t.Fatalf("version command returned error: %v", err)
	}
	want := "aeroflare version v1.2.3-test"
	if !strings.Contains(output, want) {
		t.Errorf("output = %q, want it to contain %q", output, want)
	}
	if strings.Contains(output, " (") {
		t.Errorf("output = %q, want it not to contain a parenthetical when build.Date is empty", output)
	}
}

func TestVersionCmdPrintsBuildDate(t *testing.T) {
	originalVersion := build.Version
	originalDate := build.Date
	build.Version = "v1.2.3-test"
	build.Date = "2026-07-11"
	defer func() {
		build.Version = originalVersion
		build.Date = originalDate
	}()

	output, err := executeCommand(rootCmd, "version")
	if err != nil {
		t.Fatalf("version command returned error: %v", err)
	}
	want := "aeroflare version v1.2.3-test (2026-07-11)"
	if !strings.Contains(output, want) {
		t.Errorf("output = %q, want it to contain %q", output, want)
	}
}
