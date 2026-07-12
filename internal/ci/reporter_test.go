package ci

import (
	"bytes"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/push"
)

var _ push.Reporter = (*PlainReporter)(nil)

func TestPlainReporterOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainReporter(&buf, "  ")
	r.Step(2, 3, "Uploading")
	r.Uploaded("/nix/store/aaa-foo")
	r.SkippedUpstream("/nix/store/bbb-glibc")
	r.Summary("Done", [][2]string{{"uploaded", "1"}, {"skipped", "1"}})
	out := buf.String()
	for _, want := range []string{
		"[2/3] Uploading",
		"✓ uploaded  /nix/store/aaa-foo",
		"- skipped   /nix/store/bbb-glibc",
		"uploaded 1, skipped 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}
