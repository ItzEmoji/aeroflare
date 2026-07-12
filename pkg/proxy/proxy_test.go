package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newFallthroughRegistry returns a mock OCI registry (host without scheme) and
// its cleanup func. It issues anonymous tokens and serves a "cache-config"
// manifest (carrying a public key), but 404s every per-package manifest, so
// native-mode narinfo/NAR lookups fail fast and hermetically fall through to the
// upstream cache instead of reaching a real registry.
func newFallthroughRegistry(t *testing.T) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/token":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"mock-token"}`))
		case strings.HasSuffix(r.URL.Path, "/manifests/cache-config"):
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": {"digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", "size": 2}, "layers": [], "annotations": {"aeroflare.public-key": "test-public-key"}}`))
		case strings.HasPrefix(r.URL.Path, "/v2/") && strings.Contains(r.URL.Path, "/blobs/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return strings.TrimPrefix(srv.URL, "http://"), srv.Close
}

func TestProxyServerEndpoints(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nar/test-upstream-nar.nar.xz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mock-upstream-nar"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	reg, closeReg := newFallthroughRegistry(t)
	defer closeReg()

	ps := &ProxyServer{
		Port:            37515,
		ListenAddr:      "127.0.0.1",
		Registry:        reg,
		Repository:      "test-repo",
		UpstreamCaches:  []string{mockUpstream.URL},
		TokenMgr:        NewTokenManager(reg, "test-repo", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Minute},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
	}

	// 1. Test /nix-cache-info
	req := httptest.NewRequest("GET", "/nix-cache-info", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/x-nix-cache-info" {
		t.Errorf("Expected Content-Type text/x-nix-cache-info, got %s", ct)
	}

	// 2. Test /public-key
	req = httptest.NewRequest("GET", "/public-key", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Expected Content-Type text/plain, got %s", ct)
	}

	// 3. Test /_status
	req = httptest.NewRequest("GET", "/_status", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}
	if status["mode"] != "native" {
		t.Errorf("Expected mode native, got %v", status["mode"])
	}

	// 4. Test nonexistent .narinfo (native lookup 404s, upstream 404s)
	req = httptest.NewRequest("GET", "/nonexistent.narinfo", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for nonexistent narinfo, got %d", resp.StatusCode)
	}

	// 5. Test /nar/ streaming from upstream
	req = httptest.NewRequest("GET", "/nar/test-upstream-nar.nar.xz", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	if string(bodyBytes) != "mock-upstream-nar" {
		t.Errorf("Expected mock-upstream-nar, got %s", string(bodyBytes))
	}
}

// TestProxyServer_ServeNativeNarinfo reconstructs a narinfo from a native-mode
// OCI manifest whose metadata is carried in aeroflare.* manifest annotations
// (the keys the push side actually writes). Regression test for the proxy
// previously reading vnd.aeroflare.nar.* keys, which never matched.
func TestProxyServer_ServeNativeNarinfo(t *testing.T) {
	const storeHash = "xn2nlmvng2im9mgrq46y3wkbz4ll1hnp"

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "native-token"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/"+storeHash) {
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"schemaVersion": 2,
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"config": {"digest": "sha256:0f17530206d5378c1c5d4b08ff9ec7556468da4e64d0d60ef219c8b308d2f291"},
				"layers": [
					{"mediaType": "application/vnd.aeroflare.nar.v1+zstd", "digest": "sha256:b0078e30265597a3e2fc42d1d10c9f14ba0462f0c8bf969ff5fc18b7323bcbb7", "size": 12790065}
				],
				"annotations": {
					"aeroflare.narsize": "20202056",
					"aeroflare.storepath": "/nix/store/xn2nlmvng2im9mgrq46y3wkbz4ll1hnp-snacks-nvim-e6fd58c8",
					"aeroflare.narhash": "sha256:06v4v63xc818bc4csj49ri30my24hmpddhr2a2452q7jm10ijaim",
					"aeroflare.url": "nar/xn2nlmvng2im9mgrq46y3wkbz4ll1hnp.nar.zst",
					"aeroflare.compression": "zstd",
					"aeroflare.deriver": "74n7dgfk6y0xgsy4qrlxbfpa429f259g-snacks-nvim-e6fd58c8.drv",
					"aeroflare.filehash": "sha256:1dyb7crbf67wyngrdgy8y1i09fhlkw6d3la2zkia75sm4qq8w1xh",
					"aeroflare.references": "",
					"aeroflare.filesize": "12790065"
				}
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "itzemoji2/cache-3",
		TokenMgr:        NewTokenManager(reg, "itzemoji2/cache-3", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{},
	}

	req := httptest.NewRequest("GET", "/"+storeHash+".narinfo", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for native narinfo, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/x-nix-narinfo" {
		t.Errorf("Expected Content-Type text/x-nix-narinfo, got %s", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	got := string(body)

	wants := []string{
		"StorePath: /nix/store/xn2nlmvng2im9mgrq46y3wkbz4ll1hnp-snacks-nvim-e6fd58c8\n",
		"URL: nar/xn2nlmvng2im9mgrq46y3wkbz4ll1hnp.nar.zst\n",
		"Compression: zstd\n",
		"FileHash: sha256:1dyb7crbf67wyngrdgy8y1i09fhlkw6d3la2zkia75sm4qq8w1xh\n",
		"FileSize: 12790065\n",
		"NarHash: sha256:06v4v63xc818bc4csj49ri30my24hmpddhr2a2452q7jm10ijaim\n",
		"NarSize: 20202056\n",
		"Deriver: 74n7dgfk6y0xgsy4qrlxbfpa429f259g-snacks-nvim-e6fd58c8.drv\n",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("narinfo missing %q\nfull body:\n%s", want, got)
		}
	}
}

func TestBootstrapConfig(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-bearer-token"}`))
		case "/v2/test-repo/manifests/cache-config":
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"schemaVersion": 2,
				"config": {
					"mediaType": "application/vnd.oci.empty.v1+json",
					"digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
					"size": 2
				},
				"annotations": {
					"aeroflare.public-key": "remote-key-data",
					"aeroflare.worker-url": "https://remote-worker.dev",
					"aeroflare.upstream-caches": "https://cache.nixos.org,https://nix-community.cachix.org"
				},
				"layers": [
					{
						"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
						"digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
						"size": 100
					}
				]
			}`))
		case "/v2/test-repo/blobs/sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockRegistry.Close()

	u := mockRegistry.URL
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")

	tokenMgr := NewTokenManager(u, "test-repo", "")
	conf, err := BootstrapConfig(context.Background(), nil, u, "test-repo", tokenMgr)
	if err != nil {
		t.Fatalf("Failed to bootstrap config: %v", err)
	}

	if conf.WorkerURL != "https://remote-worker.dev" {
		t.Errorf("Expected WorkerURL https://remote-worker.dev, got %s", conf.WorkerURL)
	}
	if conf.PublicKey != "remote-key-data" {
		t.Errorf("Expected PublicKey remote-key-data, got %s", conf.PublicKey)
	}
	if len(conf.UpstreamCaches) != 2 || conf.UpstreamCaches[1] != "https://nix-community.cachix.org" {
		t.Errorf("Expected UpstreamCaches slice length 2 and community cache, got %v", conf.UpstreamCaches)
	}
}

// TestBootstrapConfig_ManifestNotFound verifies that BootstrapConfig returns an error when the manifest is not found.
func TestBootstrapConfig_ManifestNotFound(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-token"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(u, "test-repo", "")
	_, err := BootstrapConfig(context.Background(), nil, u, "test-repo", tokenMgr)
	if err == nil {
		t.Fatal("Expected error when manifest returns 404, got nil")
	}
}

// TestProxyHandler_MethodNotAllowed verifies that unsupported HTTP methods return 405.
func TestProxyHandler_MethodNotAllowed(t *testing.T) {
	ps := &ProxyServer{
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test", ""),
	}

	for _, method := range []string{"DELETE", "PUT", "PATCH"} {
		req := httptest.NewRequest(method, "/nix-cache-info", nil)
		w := httptest.NewRecorder()
		ps.Handler(w, req)
		if w.Result().StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected 405 for %s, got %d", method, w.Result().StatusCode)
		}
	}
}

// TestProxyHandler_UnknownGetPath verifies that GET requests for unknown paths return 404.
func TestProxyHandler_UnknownGetPath(t *testing.T) {
	ps := &ProxyServer{
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test", ""),
	}

	req := httptest.NewRequest("GET", "/unknown/path", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown GET path, got %d", w.Result().StatusCode)
	}
}

// TestProxyHandler_Post verifies that POST requests are rejected with 405: the
// proxy is read-only (GET/HEAD), with no cache-refresh or write endpoints.
func TestProxyHandler_Post(t *testing.T) {
	ps := &ProxyServer{
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test", ""),
	}

	req := httptest.NewRequest("POST", "/unknown/post", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for POST, got %d", w.Result().StatusCode)
	}
}

// TestProxyServer_ServePublicKey_NotFound verifies that /public-key returns 404
// when the registry has no cache-config manifest (so no key is configured).
func TestProxyServer_ServePublicKey_NotFound(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-token"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test",
		HttpClient:      &http.Client{Timeout: 5 * time.Second},
		HttpShortClient: &http.Client{Timeout: 5 * time.Second},
		TokenMgr:        NewTokenManager(reg, "test", ""),
	}

	req := httptest.NewRequest("GET", "/public-key", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 when no public key is configured, got %d", w.Result().StatusCode)
	}
}

// TestProxyServer_ServePublicKey_FromCacheConfig verifies that /public-key reads
// the key straight from the cache-config manifest annotations (trimming
// surrounding whitespace), with no caching in between.
func TestProxyServer_ServePublicKey_FromCacheConfig(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-token"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/cache-config") {
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": {"digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", "size": 2}, "layers": [], "annotations": {"public-key": "  server-configured-key  "}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test",
		HttpClient:      &http.Client{Timeout: 5 * time.Second},
		HttpShortClient: &http.Client{Timeout: 5 * time.Second},
		TokenMgr:        NewTokenManager(reg, "test", ""),
	}

	req := httptest.NewRequest("GET", "/public-key", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "server-configured-key\n" {
		t.Errorf("Expected 'server-configured-key\\n', got %q", string(body))
	}
}

// TestProxyServer_ServeNar_BlobStream verifies that /nar/ streams from the OCI
// registry using the digest resolved from the native OCI manifest.
func TestProxyServer_ServeNar_BlobStream(t *testing.T) {
	narContent := []byte("fake-nar-blob-content")

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "nar-token"}`))
			return
		}
		// native mode: the NAR basename's store hash ("cached") is the image tag.
		if strings.HasSuffix(r.URL.Path, "/manifests/cached") {
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"layers": [{"digest": "sha256:narblob123"}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/blobs/sha256:narblob123") {
			w.Header().Set("Content-Length", "21")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(narContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test-repo",
		TokenMgr:        NewTokenManager(reg, "test-repo", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{},
	}

	req := httptest.NewRequest("GET", "/nar/cached.nar.xz", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for cached NAR, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(narContent) {
		t.Errorf("Expected NAR content %q, got %q", string(narContent), string(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-xz" {
		t.Errorf("Expected Content-Type application/x-xz for .xz file, got %s", ct)
	}
}

// TestProxyServer_ServeNar_NotFound verifies that /nar/ returns 404 when not found in index or upstream.
func TestProxyServer_ServeNar_NotFound(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	reg, closeReg := newFallthroughRegistry(t)
	defer closeReg()

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test-repo",
		TokenMgr:        NewTokenManager(reg, "test", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{mockUpstream.URL},
	}

	req := httptest.NewRequest("GET", "/nar/missing.nar", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for missing NAR, got %d", w.Result().StatusCode)
	}
}

func TestProxyServer_ServeNar_Upstream_Success(t *testing.T) {
	narContent := []byte("upstream-nar-blob-content")
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/nar/found.nar.xz") {
			w.Header().Set("Content-Length", strconv.Itoa(len(narContent)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(narContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	reg, closeReg := newFallthroughRegistry(t)
	defer closeReg()

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test-repo",
		TokenMgr:        NewTokenManager(reg, "test", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{mockUpstream.URL},
	}

	req := httptest.NewRequest("GET", "/nar/found.nar.xz", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for missing NAR found in upstream, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(narContent) {
		t.Errorf("Expected NAR content %q, got %q", string(narContent), string(body))
	}
}

func TestProxyServer_ServeNar_Upstream_Interrupted(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer mockUpstream.Close()

	reg, closeReg := newFallthroughRegistry(t)
	defer closeReg()

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test-repo",
		TokenMgr:        NewTokenManager(reg, "test", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{mockUpstream.URL},
	}

	req := httptest.NewRequest("GET", "/nar/interrupted.nar.xz", nil)
	w := httptest.NewRecorder()

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	// An interrupted upstream stream must abort the response (via
	// http.ErrAbortHandler, which net/http suppresses) so the client never
	// mistakes a truncated body for a complete download.
	panicked := func() (p interface{}) {
		defer func() { p = recover() }()
		ps.Handler(w, req)
		return nil
	}()

	_ = wPipe.Close()
	os.Stderr = oldStderr
	var stderrOutput bytes.Buffer
	_, _ = io.Copy(&stderrOutput, rPipe)

	if panicked != http.ErrAbortHandler {
		t.Errorf("Expected handler to abort with http.ErrAbortHandler, got: %v", panicked)
	}

	if !strings.Contains(stderrOutput.String(), "Warning: stream interrupted for upstream path") {
		t.Errorf("Expected warning in stderr, got: %s", stderrOutput.String())
	}
}

// TestTokenManager_GetToken_OciTokenEnv verifies that the oci_token env var bypasses fetching.
func TestTokenManager_GetToken_OverrideUsedDirectly(t *testing.T) {
	tokenMgr := NewTokenManager("ghcr.io", "test", "")
	tokenMgr.SetOverrideToken("direct-oci-token-value")

	token, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token != "direct-oci-token-value" {
		t.Errorf("Expected direct-oci-token-value, got %s", token)
	}
}

// TestTokenManager_GetToken_OciTokenEnv_GhpPrefix verifies that oci_token with ghp_ prefix is not used directly.
func TestTokenManager_GetToken_OciTokenEnv_GhpPrefix(t *testing.T) {
	t.Setenv("oci_token", "ghp_mypersonalaccesstoken")
	t.Setenv("NIXCACHE_TOKEN", "")

	// Point to a server that returns a valid token via exchange
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "exchanged-token"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test", "")
	token, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	// ghp_ prefix means it should be exchanged, not returned directly
	if token == "ghp_mypersonalaccesstoken" {
		t.Error("Token starting with ghp_ should not be returned directly from env")
	}
	if token != "exchanged-token" {
		t.Errorf("Expected exchanged-token, got %s", token)
	}
}

// TestTokenManager_GetToken_NixcacheTokenEnv verifies that NIXCACHE_TOKEN env var bypasses fetching.
// The env-var lookup that used to live here (oci_token / NIXCACHE_TOKEN) now
// belongs to the CLI layer; see TestRegistryOverrideToken in pkg/cmdutil.

// TestTokenManager_GetToken_Cached verifies that subsequent calls return the cached token.
func TestTokenManager_GetToken_Cached(t *testing.T) {
	t.Setenv("oci_token", "")
	t.Setenv("NIXCACHE_TOKEN", "")

	callCount := 0
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			callCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "cached-token-value"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test", "")

	// First call fetches
	token1, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		t.Fatalf("First GetToken call failed: %v", err)
	}
	// Second call should use cache
	token2, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		t.Fatalf("Second GetToken call failed: %v", err)
	}
	if token1 != "cached-token-value" || token2 != "cached-token-value" {
		t.Errorf("Expected both tokens to be cached-token-value, got %q and %q", token1, token2)
	}
	if callCount != 1 {
		t.Errorf("Expected token endpoint to be called exactly once, got %d", callCount)
	}
}

// TestTokenManager_GetToken_WithGithubToken verifies that githubToken is used for basic auth exchange.
func TestTokenManager_GetToken_WithGithubToken(t *testing.T) {
	t.Setenv("oci_token", "")
	t.Setenv("NIXCACHE_TOKEN", "")

	var receivedAuth string
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			user, pass, ok := r.BasicAuth()
			if ok {
				receivedAuth = user + ":" + pass
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "github-exchanged-token"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test", "my-github-pat")
	token, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken with github token failed: %v", err)
	}
	if token != "github-exchanged-token" {
		t.Errorf("Expected github-exchanged-token, got %s", token)
	}
	if receivedAuth != "token:my-github-pat" {
		t.Errorf("Expected basic auth token:my-github-pat, got %s", receivedAuth)
	}
}

// TestProxyServer_NixCacheInfo_Content verifies the exact content of the /nix-cache-info response.
func TestProxyServer_NixCacheInfo_Content(t *testing.T) {
	ps := &ProxyServer{
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test", ""),
	}

	req := httptest.NewRequest("GET", "/nix-cache-info", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()

	body, _ := io.ReadAll(resp.Body)
	expected := "StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n"
	if string(body) != expected {
		t.Errorf("Expected nix-cache-info body %q, got %q", expected, string(body))
	}
}

// TestProxyServer_ServeStatus_WorkerMode verifies that /_status with a workerURL shows managed-by-D1 index_generated.

// TestProxyServer_NarInfo_FallsBackToUpstream verifies narinfo falls back to upstream when not in index.
func TestProxyServer_NarInfo_FallsBackToUpstream(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upstream-hash.narinfo" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("StorePath: /nix/store/upstream-hash-foo\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	reg, closeReg := newFallthroughRegistry(t)
	defer closeReg()

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test",
		UpstreamCaches:  []string{mockUpstream.URL},
		HttpClient:      &http.Client{Timeout: 10 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		TokenMgr:        NewTokenManager(reg, "test", ""),
	}

	req := httptest.NewRequest("GET", "/upstream-hash.narinfo", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 from upstream fallback, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "upstream-hash") {
		t.Errorf("Expected upstream narinfo content, got %s", string(body))
	}
}
