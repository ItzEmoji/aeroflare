package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// TestStreamNar_TruncatedUpstreamAbortsResponse verifies that when the registry
// connection dies mid-stream, the proxy aborts its own response instead of
// terminating it cleanly — a clean termination would make a truncated NAR look
// like a complete download to the client.
func TestStreamNar_TruncatedUpstreamAbortsResponse(t *testing.T) {
	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	// Mock registry: sends partial blob data then kills the connection.
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "/blobs/"):
			_, _ = w.Write([]byte("partial data"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // abrupt connection close, no clean end
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockRegistry.Close()

	registryHost := strings.TrimPrefix(mockRegistry.URL, "http://")
	ps := newTestProxy(t, registryHost, "test-repo")

	hash, err := v1.NewHash(digest)
	if err != nil {
		t.Fatal(err)
	}
	nar := v1.Descriptor{Digest: hash, Size: 1024}

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ps.streamNar(context.Background(), w, nar, "application/x-nix-nar", "GET")
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
