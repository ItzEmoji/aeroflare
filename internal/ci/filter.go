package ci

import (
	"context"

	"github.com/itzemoji/aeroflare/internal/prepare/store"
)

// upstreamChecker reports which store path hashes an upstream cache already
// holds. Satisfied by *cache.Group.
type upstreamChecker interface {
	ExistsBatch(ctx context.Context, hashes []string, workers int) (map[string]bool, error)
}

// filterRoots drops the build outputs an upstream cache already serves.
//
// prepare filters a path's references but never the path itself, so without
// this a root already on cache.nixos.org is uploaded every run. A nil checker
// (upstream-cache: none) keeps everything. A path whose hash will not parse is
// kept: re-uploading a path we cannot classify is wasteful, dropping it is wrong.
func filterRoots(ctx context.Context, roots []string, checker upstreamChecker, workers int) (kept []string, skipped int, err error) {
	if checker == nil || len(roots) == 0 {
		return roots, 0, nil
	}

	hashByPath := make(map[string]string, len(roots))
	hashes := make([]string, 0, len(roots))
	for _, path := range roots {
		hash, _, parseErr := store.ParsePath(path)
		if parseErr != nil {
			continue
		}
		hashByPath[path] = hash
		hashes = append(hashes, hash)
	}

	exists, err := checker.ExistsBatch(ctx, hashes, workers)
	if err != nil {
		return nil, 0, err
	}

	for _, path := range roots {
		if hash, ok := hashByPath[path]; ok && exists[hash] {
			skipped++
			continue
		}
		kept = append(kept, path)
	}
	return kept, skipped, nil
}
