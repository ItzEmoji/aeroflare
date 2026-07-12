package iostreams

import (
	"os"
	"testing"
)

func TestPrintHelpersWriteToTheRightStream(t *testing.T) {
	io, _, out, errOut := Test()

	io.Error("boom")
	io.Success("yay")
	io.Info("fyi")
	io.Warning("careful")

	if got, want := errOut.String(), "Error: boom\n"; got != want {
		t.Errorf("ErrOut = %q, want %q", got, want)
	}
	if got, want := out.String(), "yay\nfyi\nWarning: careful\n"; got != want {
		t.Errorf("Out = %q, want %q", got, want)
	}
}

func TestIsStdinTTY(t *testing.T) {
	t.Run("override wins when set", func(t *testing.T) {
		io, _, _, _ := Test()

		io.SetStdinTTY(true)
		if !io.IsStdinTTY() {
			t.Error("IsStdinTTY() = false after SetStdinTTY(true), want true")
		}

		io.SetStdinTTY(false)
		if io.IsStdinTTY() {
			t.Error("IsStdinTTY() = true after SetStdinTTY(false), want false")
		}
	})

	t.Run("buffer-backed streams are not a TTY", func(t *testing.T) {
		// Test() sets no override and no *os.File, so detection must fall
		// through to false rather than panicking on a nil file.
		io, _, _, _ := Test()
		if io.IsStdinTTY() {
			t.Error("IsStdinTTY() = true for buffer-backed streams, want false")
		}
	})

	t.Run("a regular file is not a character device", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()

		io := System()
		io.SetStdinFile(f)

		if io.IsStdinTTY() {
			t.Error("IsStdinTTY() = true for a regular file, want false")
		}
	})
}
