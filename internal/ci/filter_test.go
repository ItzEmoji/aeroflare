package ci

import (
	"context"
	"errors"
	"testing"
)

// fakeChecker reports the configured hashes as upstream, or fails outright.
type fakeChecker struct {
	present map[string]bool
	err     error
	calls   int
}

func (f *fakeChecker) ExistsBatch(ctx context.Context, hashes []string, workers int) (map[string]bool, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		if f.present[h] {
			out[h] = true
		}
	}
	return out, nil
}

const (
	pathA = "/nix/store/00000000000000000000000000000000-a"
	pathB = "/nix/store/11111111111111111111111111111111-b"
)

func TestFilterRoots_DropsPathsAlreadyUpstream(t *testing.T) {
	f := &fakeChecker{present: map[string]bool{"00000000000000000000000000000000": true}}
	kept, skipped, err := filterRoots(context.Background(), []string{pathA, pathB}, f, 4)
	if err != nil {
		t.Fatalf("filterRoots: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(kept) != 1 || kept[0] != pathB {
		t.Errorf("kept = %v, want [%s]", kept, pathB)
	}
}

func TestFilterRoots_KeepsEverythingWhenNothingUpstream(t *testing.T) {
	f := &fakeChecker{present: map[string]bool{}}
	kept, skipped, err := filterRoots(context.Background(), []string{pathA, pathB}, f, 4)
	if err != nil {
		t.Fatalf("filterRoots: %v", err)
	}
	if skipped != 0 || len(kept) != 2 {
		t.Errorf("kept = %v, skipped = %d; want both kept", kept, skipped)
	}
}

func TestFilterRoots_DropsAllWhenAllUpstream(t *testing.T) {
	f := &fakeChecker{present: map[string]bool{
		"00000000000000000000000000000000": true,
		"11111111111111111111111111111111": true,
	}}
	kept, skipped, err := filterRoots(context.Background(), []string{pathA, pathB}, f, 4)
	if err != nil {
		t.Fatalf("filterRoots: %v", err)
	}
	if len(kept) != 0 || skipped != 2 {
		t.Errorf("kept = %v, skipped = %d; want none kept", kept, skipped)
	}
}

// An unparseable path cannot be classified, so it must be uploaded. Dropping it
// would silently lose a build output.
func TestFilterRoots_KeepsUnparseablePath(t *testing.T) {
	// Use "/nix/store/nodash" which has no dash in the basename, so ParsePath fails.
	unparseable := "/nix/store/nodash"
	// Populate present with a hash that would skip a path if it parsed. Since
	// the path is unparseable, it's kept despite a matching hash being available,
	// proving the parseErr branch is taken.
	f := &fakeChecker{present: map[string]bool{"00000000000000000000000000000000": true}}
	kept, _, err := filterRoots(context.Background(), []string{unparseable}, f, 4)
	if err != nil {
		t.Fatalf("filterRoots: %v", err)
	}
	if len(kept) != 1 || kept[0] != unparseable {
		t.Errorf("kept = %v, want the unparseable path retained", kept)
	}
}

func TestFilterRoots_NilCheckerKeepsEverything(t *testing.T) {
	kept, skipped, err := filterRoots(context.Background(), []string{pathA, pathB}, nil, 4)
	if err != nil {
		t.Fatalf("filterRoots: %v", err)
	}
	if len(kept) != 2 || skipped != 0 {
		t.Errorf("kept = %v, skipped = %d; want both kept", kept, skipped)
	}
}

func TestFilterRoots_PropagatesCheckerError(t *testing.T) {
	f := &fakeChecker{err: errors.New("boom")}
	if _, _, err := filterRoots(context.Background(), []string{pathA}, f, 4); err == nil {
		t.Fatal("expected the checker error to propagate")
	}
}
