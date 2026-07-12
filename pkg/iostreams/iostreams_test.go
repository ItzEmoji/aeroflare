package iostreams

import "testing"

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
