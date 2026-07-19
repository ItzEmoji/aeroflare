package version

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
)

func TestVersionRun(t *testing.T) {
	io, _, out, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: io, AppVersion: "1.2.3"}

	cmd := NewCmdVersion(f, "1.2.3", "2026-07-12")
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	got := out.String()
	if !strings.Contains(got, "aeroflare version 1.2.3") {
		t.Errorf("output %q missing version", got)
	}
	if !strings.Contains(got, "(2026-07-12)") {
		t.Errorf("output %q missing build date", got)
	}
}

func TestVersionRunWithoutBuildDate(t *testing.T) {
	io, _, out, _ := iostreams.Test()
	f := &cmdutil.Factory{IOStreams: io}

	cmd := NewCmdVersion(f, "1.2.3", "")
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if got, want := out.String(), "aeroflare version 1.2.3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
