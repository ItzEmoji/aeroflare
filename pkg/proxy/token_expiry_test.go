package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTokenRegistry(t *testing.T, fetches *atomic.Int32, expiresIn int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			n := fetches.Add(1)
			w.Header().Set("Content-Type", "application/json")
			body := fmt.Sprintf(`{"token":"token-%d"`, n)
			if expiresIn > 0 {
				body += fmt.Sprintf(`,"expires_in":%d`, expiresIn)
			}
			body += "}"
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestTokenManager_HonorsExpiresIn(t *testing.T) {
	var fetches atomic.Int32
	srv := newTokenRegistry(t, &fetches, 600) // registry says 10 minutes
	defer srv.Close()

	registryHost := strings.TrimPrefix(srv.URL, "http://")
	tm := NewTokenManager(registryHost, "test-repo", "")

	now := time.Now()
	tm.now = func() time.Time { return now }

	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 5 minutes later: with the old hardcoded 4-minute cache this would
	// re-fetch; with expires_in=600 honored the token is still valid.
	now = now.Add(5 * time.Minute)
	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("token with expires_in=600 was re-fetched after 5min; want 1 fetch, got %d", fetches.Load())
	}

	// Past expiry (minus safety margin) it must re-fetch.
	now = now.Add(6 * time.Minute)
	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("expired token was not re-fetched; want 2 fetches, got %d", fetches.Load())
	}
}

func TestTokenManager_DefaultExpiryWithoutExpiresIn(t *testing.T) {
	var fetches atomic.Int32
	srv := newTokenRegistry(t, &fetches, 0) // no expires_in in response
	defer srv.Close()

	registryHost := strings.TrimPrefix(srv.URL, "http://")
	tm := NewTokenManager(registryHost, "test-repo", "")

	now := time.Now()
	tm.now = func() time.Time { return now }

	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(1 * time.Minute)
	if _, err := tm.GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("want 1 fetch within default TTL, got %d", fetches.Load())
	}
}

func TestTokenManager_OverrideTokenIsRaceFree(t *testing.T) {
	tm := NewTokenManager("example.com", "test-repo", "")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tm.SetOverrideToken("static-token")
		}()
		go func() {
			defer wg.Done()
			_, _ = tm.GetToken(context.Background())
		}()
	}
	wg.Wait()
}
