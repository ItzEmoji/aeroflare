package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamBlob_TruncatedUpstreamAbortsResponse verifies that when the
// registry connection dies mid-stream, the proxy aborts its own response
// instead of terminating it cleanly — a clean termination would make a
// truncated NAR look like a complete download to the client.
func TestStreamBlob_TruncatedUpstreamAbortsResponse(t *testing.T) {
	digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	// Mock registry: sends partial blob data then kills the connection.
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/blobs/") {
			_, _ = w.Write([]byte("partial data"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // abrupt connection close, no clean end
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	registryHost := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(registryHost, "test-repo", "")
	tokenMgr.SetOverrideToken("mock-token")

	ps := &ProxyServer{
		Registry:   registryHost,
		Repository: "test-repo",
		TokenMgr:   tokenMgr,
		HttpClient: &http.Client{Timeout: 10 * time.Second},
	}

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ps.streamBlob(context.Background(), w, digest, "application/x-nix-nar", "GET")
	}))
	defer front.Close()

	resp, err := http.Get(front.URL + "/nar/whatever.nar.xz")
	if err != nil {
		// The abort may surface at response time; that also signals failure
		// to the client, which is acceptable.
		return
	}
	defer func() { _ = resp.Body.Close() }()

	_, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatal("truncated upstream stream produced a cleanly-terminated response; client would treat a truncated NAR as complete")
	}
}
