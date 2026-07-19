package push

import (
	"sort"

	"github.com/itzemoji/aeroflare/internal/backend"
	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
)

// completeReceipts filters receipts down to store paths whose full closure
// (the path itself plus every prepared missing reference, transitively) was
// uploaded. byPath maps store paths to their upload receipts; absence means
// the upload failed. It returns the receipts safe to index and the store
// paths excluded because their closure is incomplete.
func completeReceipts(results []*prepare.Result, byPath map[string]backend.PushReceipt) ([]backend.PushReceipt, []string) {
	memo := make(map[string]bool)

	var complete func(r *prepare.Result) bool
	complete = func(r *prepare.Result) bool {
		if done, ok := memo[r.StorePath]; ok {
			return done
		}
		// Mark in-progress to terminate on reference cycles; a cycle member
		// is complete if uploaded and all its other refs are.
		memo[r.StorePath] = true
		ok := true
		if _, uploaded := byPath[r.StorePath]; !uploaded {
			ok = false
		}
		for _, ref := range r.MissingRefResults {
			if !complete(ref) {
				ok = false
			}
		}
		memo[r.StorePath] = ok
		return ok
	}

	var receipts []backend.PushReceipt
	var excluded []string
	seen := make(map[string]bool)

	var walk func(r *prepare.Result)
	walk = func(r *prepare.Result) {
		if seen[r.StorePath] {
			return
		}
		seen[r.StorePath] = true
		if receipt, uploaded := byPath[r.StorePath]; uploaded {
			if complete(r) {
				receipts = append(receipts, receipt)
			} else {
				excluded = append(excluded, r.StorePath)
			}
		}
		for _, ref := range r.MissingRefResults {
			walk(ref)
		}
	}
	for _, r := range results {
		walk(r)
	}

	sort.Strings(excluded)
	return receipts, excluded
}
