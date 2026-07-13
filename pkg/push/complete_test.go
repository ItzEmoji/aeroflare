package push

import (
	"testing"

	"github.com/itzemoji/aeroflare/internal/backend"
	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
)

func TestCompleteReceipts_ExcludesEntriesWithFailedRefs(t *testing.T) {
	// Root A depends on ref B (upload failed). Root C is standalone (uploaded).
	refB := &prepare.Result{StorePath: "/nix/store/bbbb-refB"}
	rootA := &prepare.Result{StorePath: "/nix/store/aaaa-rootA", MissingRefResults: []*prepare.Result{refB}}
	rootC := &prepare.Result{StorePath: "/nix/store/cccc-rootC"}

	byPath := map[string]backend.PushReceipt{
		"/nix/store/aaaa-rootA": {StorePath: "/nix/store/aaaa-rootA", IsRoot: true},
		"/nix/store/cccc-rootC": {StorePath: "/nix/store/cccc-rootC", IsRoot: true},
		// refB has no receipt: its upload failed.
	}

	receipts, excluded := completeReceipts([]*prepare.Result{rootA, rootC}, byPath)

	if len(receipts) != 1 || receipts[0].StorePath != "/nix/store/cccc-rootC" {
		t.Fatalf("expected only rootC receipt, got %+v", receipts)
	}
	if len(excluded) != 1 || excluded[0] != "/nix/store/aaaa-rootA" {
		t.Fatalf("expected rootA excluded, got %v", excluded)
	}
}

func TestCompleteReceipts_TransitiveFailurePoisonsAncestors(t *testing.T) {
	// A -> B -> C, C failed: both A and B must be excluded.
	refC := &prepare.Result{StorePath: "/nix/store/cccc-refC"}
	refB := &prepare.Result{StorePath: "/nix/store/bbbb-refB", MissingRefResults: []*prepare.Result{refC}}
	rootA := &prepare.Result{StorePath: "/nix/store/aaaa-rootA", MissingRefResults: []*prepare.Result{refB}}

	byPath := map[string]backend.PushReceipt{
		"/nix/store/aaaa-rootA": {StorePath: "/nix/store/aaaa-rootA", IsRoot: true},
		"/nix/store/bbbb-refB":  {StorePath: "/nix/store/bbbb-refB"},
	}

	receipts, excluded := completeReceipts([]*prepare.Result{rootA}, byPath)

	if len(receipts) != 0 {
		t.Fatalf("expected no receipts, got %+v", receipts)
	}
	if len(excluded) != 2 {
		t.Fatalf("expected rootA and refB excluded, got %v", excluded)
	}
}

func TestCompleteReceipts_AllCompleteIncludesEverything(t *testing.T) {
	refB := &prepare.Result{StorePath: "/nix/store/bbbb-refB"}
	rootA := &prepare.Result{StorePath: "/nix/store/aaaa-rootA", MissingRefResults: []*prepare.Result{refB}}

	byPath := map[string]backend.PushReceipt{
		"/nix/store/aaaa-rootA": {StorePath: "/nix/store/aaaa-rootA", IsRoot: true},
		"/nix/store/bbbb-refB":  {StorePath: "/nix/store/bbbb-refB"},
	}

	receipts, excluded := completeReceipts([]*prepare.Result{rootA}, byPath)

	if len(receipts) != 2 {
		t.Fatalf("expected both receipts, got %+v", receipts)
	}
	if len(excluded) != 0 {
		t.Fatalf("expected nothing excluded, got %v", excluded)
	}
}
