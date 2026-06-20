package cache

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheExists(t *testing.T) {
	existingHashes := map[string]bool{
		"abc123":                     true,
		"def456":                     true,
		"0c2j6g2bxqzw7x9q6kbx3vrrj6": true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		// Request path: /<hash>.narinfo
		hashPath := r.URL.Path[1:] // remove leading /
		hash := hashPath[:len(hashPath)-len(".narinfo")]
		if existingHashes[hash] {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := New(server.URL)

	ctx := context.Background()

	// Test existing hash
	exists, err := c.Exists(ctx, "abc123")
	if err != nil {
		t.Fatalf("Exists(abc123) error: %v", err)
	}
	if !exists {
		t.Error("expected abc123 to exist")
	}

	// Test non-existing hash
	exists, err = c.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Exists(nonexistent) error: %v", err)
	}
	if exists {
		t.Error("expected nonexistent to not exist")
	}
}

func TestCacheCaching(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL)
	ctx := context.Background()

	// First call should make a request
	_, err := c.Exists(ctx, "testhash")
	if err != nil {
		t.Fatal(err)
	}

	// Second call should be served from cache
	_, err = c.Exists(ctx, "testhash")
	if err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt64(&requestCount) != 1 {
		t.Errorf("expected 1 HTTP request, got %d", atomic.LoadInt64(&requestCount))
	}
}

func TestCacheExistsBatch(t *testing.T) {
	existingHashes := map[string]bool{
		"hash1": true,
		"hash2": true,
		"hash4": true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hashPath := r.URL.Path[1:]
		hash := hashPath[:len(hashPath)-len(".narinfo")]
		if existingHashes[hash] {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := New(server.URL)
	ctx := context.Background()

	hashes := []string{"hash1", "hash2", "hash3", "hash4", "hash5"}
	results, err := c.ExistsBatch(ctx, hashes, 3)
	if err != nil {
		t.Fatalf("ExistsBatch error: %v", err)
	}

	for _, h := range hashes {
		expected := existingHashes[h]
		if results[h] != expected {
			t.Errorf("hash %s: got %v, want %v", h, results[h], expected)
		}
	}
}

func TestCacheConcurrentDedup(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		time.Sleep(10 * time.Millisecond) // slow response to encourage concurrent dedup
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL)
	ctx := context.Background()

	// Launch multiple concurrent checks for the same hash
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Exists(ctx, "samehash")
			if err != nil {
				t.Errorf("Exists error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Due to in-flight deduplication, only 1 request should be made
	if count := atomic.LoadInt64(&requestCount); count != 1 {
		t.Errorf("expected 1 HTTP request due to dedup, got %d", count)
	}
}

func TestCacheTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, WithTimeout(50*time.Millisecond))
	ctx := context.Background()

	_, err := c.Exists(ctx, "slowhash")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestCacheUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := New(server.URL)
	ctx := context.Background()

	_, err := c.Exists(ctx, "errorhash")
	if err == nil {
		t.Error("expected error for 500 status")
	}
	if fmt.Sprintf("%v", err) == "" {
		t.Error("error should not be empty")
	}
}
