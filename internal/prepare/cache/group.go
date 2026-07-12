package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

// Group is a set of upstream binary caches queried as one. A store path hash is
// present when any member holds it.
//
// A member that fails to answer contributes no hits. The group therefore
// under-reports presence, which costs a redundant upload but never produces a
// cache with dangling references. Assuming presence on error would.
type Group struct {
	caches []*Cache
	urls   []string
	warn   io.Writer
}

// NewGroup builds a Group over baseURLs. Each option is applied to every member.
func NewGroup(baseURLs []string, opts ...Option) *Group {
	g := &Group{
		urls: append([]string(nil), baseURLs...),
		warn: os.Stderr,
	}
	for _, u := range baseURLs {
		g.caches = append(g.caches, New(u, opts...))
	}
	return g
}

// SetWarnWriter redirects per-member failure warnings. Defaults to os.Stderr.
// It is not safe to call concurrently with ExistsBatch and is intended to be called once at construction time.
func (g *Group) SetWarnWriter(w io.Writer) { g.warn = w }

// Len reports the number of member caches.
func (g *Group) Len() int { return len(g.caches) }

// ExistsBatch reports which of hashes any reachable member holds. A hash absent
// from the returned map is held by no reachable member, and must be uploaded.
// An empty group reports nothing upstream. Member failures are warnings, not
// errors: the returned error is always nil.
func (g *Group) ExistsBatch(ctx context.Context, hashes []string, workers int) (map[string]bool, error) {
	merged := make(map[string]bool, len(hashes))
	if len(g.caches) == 0 || len(hashes) == 0 {
		return merged, nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, c := range g.caches {
		wg.Add(1)
		go func(i int, c *Cache) {
			defer wg.Done()
			m, err := c.ExistsBatch(ctx, hashes, workers)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				_, _ = fmt.Fprintf(g.warn, "warning: upstream %s unavailable; its paths will be re-uploaded: %v\n", g.urls[i], err)
				return
			}
			for h, ok := range m {
				if ok {
					merged[h] = true
				}
			}
		}(i, c)
	}
	wg.Wait()
	return merged, nil
}
