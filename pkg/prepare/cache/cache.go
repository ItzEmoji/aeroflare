package cache

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Cache is a client for a Nix binary cache (e.g. https://cache.nixos.org).
// It checks whether narinfo files exist for given store path hashes.
// Results are cached in-memory to avoid redundant HTTP requests.
// Concurrent requests for the same hash are deduplicated.
type Cache struct {
	client   *http.Client
	baseURL  string
	mu       sync.Mutex
	results  map[string]bool          // hash -> exists
	inflight map[string]chan struct{} // hash -> done signal for in-flight requests
}

// Option configures a Cache.
type Option func(*Cache)

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Cache) {
		c.client.Timeout = d
	}
}

// WithMaxConns sets the maximum number of idle connections per host.
func WithMaxConns(n int) Option {
	return func(c *Cache) {
		c.client.Transport = &http.Transport{
			MaxIdleConns:        n,
			MaxIdleConnsPerHost: n,
			IdleConnTimeout:     90 * time.Second,
		}
	}
}

// New creates a new binary cache client for the given base URL.
func New(baseURL string, opts ...Option) *Cache {
	baseURL = strings.TrimRight(baseURL, "/")
	c := &Cache{
		baseURL:  baseURL,
		results:  make(map[string]bool),
		inflight: make(map[string]chan struct{}),
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Exists checks whether a narinfo exists for the given store path hash.
// The result is cached for the lifetime of the Cache instance.
// Concurrent calls for the same hash are deduplicated to a single HTTP request.
func (c *Cache) Exists(ctx context.Context, hash string) (bool, error) {
	// Fast path: check cache
	c.mu.Lock()
	if v, ok := c.results[hash]; ok {
		c.mu.Unlock()
		return v, nil
	}

	// Check if there's an in-flight request for this hash
	if ch, ok := c.inflight[hash]; ok {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		c.mu.Lock()
		v := c.results[hash]
		c.mu.Unlock()
		return v, nil
	}

	// Register our request
	ch := make(chan struct{})
	c.inflight[hash] = ch
	c.mu.Unlock()

	// Make the HTTP request
	exists, err := c.checkRemote(ctx, hash)

	c.mu.Lock()
	delete(c.inflight, hash)
	if err == nil {
		c.results[hash] = exists
	}
	close(ch)
	c.mu.Unlock()

	if err != nil {
		return false, err
	}
	return exists, nil
}

// checkRemote issues a HEAD request for the narinfo and interprets the
// response status as existence.
func (c *Cache) checkRemote(ctx context.Context, hash string) (bool, error) {
	url := fmt.Sprintf("%s/%s.narinfo", c.baseURL, hash)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("request %s: %w", url, err)
	}
	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
}

// ExistsBatch checks existence for multiple hashes concurrently.
// The workers parameter controls the number of concurrent HTTP requests.
// Results are cached individually, so subsequent calls for the same hash
// will be served from cache.
func (c *Cache) ExistsBatch(ctx context.Context, hashes []string, workers int) (map[string]bool, error) {
	if workers <= 0 {
		workers = 50
	}

	type result struct {
		hash   string
		exists bool
		err    error
	}

	jobs := make(chan string, len(hashes))
	resultsCh := make(chan result, len(hashes))

	var wg sync.WaitGroup
	if workers > len(hashes) {
		workers = len(hashes)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hash := range jobs {
				exists, err := c.Exists(ctx, hash)
				resultsCh <- result{hash: hash, exists: exists, err: err}
			}
		}()
	}

	for _, hash := range hashes {
		jobs <- hash
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	m := make(map[string]bool, len(hashes))
	for r := range resultsCh {
		if r.err != nil {
			return nil, fmt.Errorf("checking %s: %w", r.hash, r.err)
		}
		m[r.hash] = r.exists
	}

	return m, nil
}
