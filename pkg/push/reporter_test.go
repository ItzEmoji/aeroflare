package push_test

import (
	"errors"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/push"
)

// recordingReporter captures events instead of printing them. A library caller
// must be able to do exactly this: consume the push pipeline's progress without
// anything reaching the process's stdout.
type recordingReporter struct {
	warns  []string
	infos  []string
	failed []string
}

func (r *recordingReporter) Step(step, total int, msg string)         {}
func (r *recordingReporter) Uploaded(storePath string)                {}
func (r *recordingReporter) SkippedUpstream(storePath string)         {}
func (r *recordingReporter) Success(msg string)                       {}
func (r *recordingReporter) Summary(title string, fields [][2]string) {}
func (r *recordingReporter) Warn(msg string)                          { r.warns = append(r.warns, msg) }
func (r *recordingReporter) Info(msg string)                          { r.infos = append(r.infos, msg) }

func (r *recordingReporter) Failed(storePath, stage string, err error) {
	r.failed = append(r.failed, storePath)
}

// The compile-time assertion is the real check: it fails to build until Warn
// and Info are part of the Reporter interface.
var _ push.Reporter = (*recordingReporter)(nil)

func TestReporterCapturesInsteadOfPrinting(t *testing.T) {
	rec := &recordingReporter{}
	var r push.Reporter = rec

	r.Warn("upstream cache check failed: boom")
	r.Info("No new paths to push.")
	r.Failed("/nix/store/abc-foo", "failed to push NAR layer", errors.New("boom"))

	if len(rec.warns) != 1 || rec.warns[0] != "upstream cache check failed: boom" {
		t.Errorf("warns = %v, want the single warning", rec.warns)
	}
	if len(rec.infos) != 1 || rec.infos[0] != "No new paths to push." {
		t.Errorf("infos = %v, want the single info line", rec.infos)
	}
	if len(rec.failed) != 1 || rec.failed[0] != "/nix/store/abc-foo" {
		t.Errorf("failed = %v, want the single failed path", rec.failed)
	}
}
