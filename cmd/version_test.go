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
}
