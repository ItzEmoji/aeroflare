package push

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn and returns everything it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// The push engine used to print these lines itself. Now it routes them through
// Reporter and this type renders them. The rendered text must be identical to
// what the engine printed before, or the refactor silently changed the CLI's
// output.
func TestUIReporterRendersLegacyOutput(t *testing.T) {
	r := NewUIReporter(false)

	tests := []struct {
		name string
		emit func()
		want string
	}{
		{
			name: "Info renders the line as-is",
			emit: func() { r.Info("No new paths to push.") },
			want: "No new paths to push.\n",
		},
		{
			name: "Info preserves the chunk header's blank-line padding",
			emit: func() { r.Info("\n--- Processing chunk 1/2 ---\n") },
			want: "\n--- Processing chunk 1/2 ---\n\n",
		},
		{
			name: "Warn keeps the WARNING prefix",
			emit: func() { r.Warn("upstream cache check failed: boom") },
			want: "WARNING: upstream cache check failed: boom\n",
		},
		{
			// Regression guard: routing per-path failures through Warn would
			// have relabelled these from ERROR to WARNING.
			name: "Failed keeps the ERROR prefix and field order",
			emit: func() {
				r.Failed("/nix/store/abc-foo", "failed to push NAR layer", errors.New("boom"))
			},
			want: "ERROR: failed to push NAR layer (/nix/store/abc-foo): boom\n",
		},
		{
			name: "SkippedUpstream renders the skip line",
			emit: func() { r.SkippedUpstream("/nix/store/abc-foo") },
			want: "Skipping /nix/store/abc-foo (already in upstream cache)\n",
		},
		{
			name: "Step renders the [n/total] progress line",
			emit: func() { r.Step(2, 3, "Uploading 5 packages to OCI registry") },
			want: "\n  [2/3] Uploading 5 packages to OCI registry\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := captureStdout(t, tt.emit); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// Uploaded renders the basename with a lipgloss checkmark, so assert on the
// parts rather than the exact escape sequences.
func TestUIReporterUploadedRendersBasename(t *testing.T) {
	out := captureStdout(t, func() {
		NewUIReporter(false).Uploaded("/nix/store/aaaa-hello-1.0")
	})

	if !strings.Contains(out, "hello-1.0") {
		t.Errorf("output %q should contain the path's basename", out)
	}
	if strings.Contains(out, "/nix/store/") {
		t.Errorf("output %q should show only the basename, not the full store path", out)
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("output %q should contain the checkmark", out)
	}
}
