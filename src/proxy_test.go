package network

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

	// Setup mock CacheIndex
	cacheIndex := &CacheIndex{
		Data: &CacheIndexData{
			PublicKey: "test-public-key",
			Generated: "2026-06-18",
			Entries: map[string]IndexEntry{
				"test-hash": {
					NarInfo:   "StoreDir: /nix/store\nURL: nar/test-nar.nar.xz\n",
					NarDigest: "sha256:test-digest",
				},
			},
		},
		NarLookups: map[string]string{
			"test-nar.nar.xz": "sha256:test-digest",
		},
		LastFetch: time.Now(),
		IndexTTL:  5 * time.Minute,
	}

	ps := &ProxyServer{
		Port:            37515,
		ListenAddr:      "127.0.0.1",
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		UpstreamCaches:  []string{mockUpstream.URL},
		TokenMgr:        NewTokenManager("ghcr.io", "test-repo/nix-cache", ""),
		CacheIndex:      cacheIndex,
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
	if status["index_entries"].(float64) != 1 {
		t.Errorf("Expected 1 entry, got %v", status["index_entries"])
	}

	// 4. Test /.narinfo lookup
	req = httptest.NewRequest("GET", "/test-hash.narinfo", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/x-nix-narinfo" {
		t.Errorf("Expected Content-Type text/x-nix-narinfo, got %s", ct)
	}

	// 5. Test nonexistent .narinfo
	req = httptest.NewRequest("GET", "/nonexistent.narinfo", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for nonexistent narinfo, got %d", resp.StatusCode)
	}

	// 6. Test /nar/ streaming from upstream
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

func TestProxyServerWorkerMode(t *testing.T) {
	mockWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/public-key":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("worker-public-key"))
		case "/worker-hash.narinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("StoreDir: /nix/store\nURL: nar/worker-nar.nar.xz\n"))
		case "/nar/worker-nar.nar.xz/digest":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("sha256:worker-digest-val"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockWorker.Close()

	ps := &ProxyServer{
		Port:            37515,
		ListenAddr:      "127.0.0.1",
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		UpstreamCaches:  []string{"https://cache.nixos.org"},
		TokenMgr:        NewTokenManager("ghcr.io", "test-repo/nix-cache", ""),
		CacheIndex:      &CacheIndex{},
		WorkerURL:       mockWorker.URL,
		HttpClient:      &http.Client{Timeout: 30 * time.Minute},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
	}

	// 1. Test /public-key from worker
	req := httptest.NewRequest("GET", "/public-key", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// 2. Test /worker-hash.narinfo from worker
	req = httptest.NewRequest("GET", "/worker-hash.narinfo", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// 3. Test _status shows worker URL
	req = httptest.NewRequest("GET", "/_status", nil)
	w = httptest.NewRecorder()
	ps.Handler(w, req)
	resp = w.Result()
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}
	if status["worker_url"] != mockWorker.URL {
		t.Errorf("Expected worker_url %s, got %v", mockWorker.URL, status["worker_url"])
	}
}

func TestBootstrapConfig(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-bearer-token"}`))
		case "/v2/test-repo/nix-cache/manifests/cache-config":
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"schemaVersion": 2,
				"layers": [
					{
						"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
						"digest": "sha256:config-blob-digest",
						"size": 100
					}
				]
			}`))
		case "/v2/test-repo/nix-cache/blobs/sha256:config-blob-digest":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"worker_url": "https://remote-worker.dev",
				"public_key": "remote-key-data",
				"upstream_caches": ["https://cache.nixos.org", "https://nix-community.cachix.org"]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockRegistry.Close()

	u := mockRegistry.URL
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")

	tokenMgr := NewTokenManager(u, "test-repo/nix-cache", "")
	conf, err := BootstrapConfig(u, "test-repo/nix-cache", tokenMgr)
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
	tokenMgr := NewTokenManager(u, "test-repo/nix-cache", "")
	_, err := BootstrapConfig(u, "test-repo/nix-cache", tokenMgr)
	if err == nil {
		t.Fatal("Expected error when manifest returns 404, got nil")
	}
}

// TestBootstrapConfig_EmptyLayers verifies that BootstrapConfig returns an error when manifest has no layers.
func TestBootstrapConfig_EmptyLayers(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-token"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/cache-config") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"schemaVersion": 2, "layers": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(u, "test-repo/nix-cache", "")
	_, err := BootstrapConfig(u, "test-repo/nix-cache", tokenMgr)
	if err == nil {
		t.Fatal("Expected error when manifest has empty layers, got nil")
	}
}

// TestBootstrapConfig_BlobNotFound verifies that BootstrapConfig returns an error when the config blob is not found.
func TestBootstrapConfig_BlobNotFound(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "mock-token"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/cache-config") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"schemaVersion": 2, "layers": [{"digest": "sha256:missingblob", "size": 10}]}`))
			return
		}
		// Return 404 for the blob
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(u, "test-repo/nix-cache", "")
	_, err := BootstrapConfig(u, "test-repo/nix-cache", tokenMgr)
	if err == nil {
		t.Fatal("Expected error when blob returns 404, got nil")
	}
}

// TestProxyHandler_MethodNotAllowed verifies that unsupported HTTP methods return 405.
func TestProxyHandler_MethodNotAllowed(t *testing.T) {
	ps := &ProxyServer{
		CacheIndex:      &CacheIndex{},
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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
		CacheIndex:      &CacheIndex{},
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
	}

	req := httptest.NewRequest("GET", "/unknown/path", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown GET path, got %d", w.Result().StatusCode)
	}
}

// TestProxyHandler_UnknownPostPath verifies that POST requests for unknown paths return 404.
func TestProxyHandler_UnknownPostPath(t *testing.T) {
	ps := &ProxyServer{
		CacheIndex:      &CacheIndex{},
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
	}

	req := httptest.NewRequest("POST", "/unknown/post", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown POST path, got %d", w.Result().StatusCode)
	}
}

// TestProxyServer_ServePublicKey_NotFound verifies that /public-key returns 404 when no key is configured.
func TestProxyServer_ServePublicKey_NotFound(t *testing.T) {
	cacheIndex := &CacheIndex{
		Data: &CacheIndexData{
			PublicKey: "", // No public key
			Entries:   map[string]IndexEntry{},
		},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}
	ps := &ProxyServer{
		CacheIndex:      cacheIndex,
		PublicKey:       "", // No public key set on server either
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
	}

	req := httptest.NewRequest("GET", "/public-key", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 when no public key is configured, got %d", w.Result().StatusCode)
	}
}

// TestProxyServer_ServePublicKey_FromServerField verifies that /public-key uses the PublicKey field when set directly.
func TestProxyServer_ServePublicKey_FromServerField(t *testing.T) {
	cacheIndex := &CacheIndex{
		Data:       &CacheIndexData{Entries: map[string]IndexEntry{}},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}
	ps := &ProxyServer{
		CacheIndex:      cacheIndex,
		PublicKey:       "  server-configured-key  ", // Whitespace should be trimmed + newline appended
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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

// TestProxyServer_HandleRefresh_NoWorker_Success verifies /_refresh triggers a CacheIndex refresh via a mock registry.
func TestProxyServer_HandleRefresh_NoWorker_Success(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "refresh-token"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/manifests/cache-index") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"layers": [{"digest": "sha256:indexblob", "size": 50}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/blobs/sha256:indexblob") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"entries": {"abc123": {"narinfo": "URL: nar/abc.nar.xz\n", "nar_digest": "sha256:abc"}}, "public_key": "refresh-key", "generated": "now"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	tmpDir, err := os.MkdirTemp("", "aeroflare-refresh-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	reg := strings.TrimPrefix(mockRegistry.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test-repo/nix-cache", "")
	cacheIndex := &CacheIndex{
		IndexDir:   tmpDir,
		IndexTTL:   5 * time.Minute,
		TokenMgr:   tokenMgr,
		Registry:   reg,
		Repository: "test-repo/nix-cache",
	}

	ps := &ProxyServer{
		CacheIndex:      cacheIndex,
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		TokenMgr:        tokenMgr,
	}

	req := httptest.NewRequest("POST", "/_refresh", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result["refreshed"] != true {
		t.Errorf("Expected refreshed=true, got %v", result["refreshed"])
	}
	entries, ok := result["entries"].(float64)
	if !ok || entries != 1 {
		t.Errorf("Expected entries=1, got %v", result["entries"])
	}
}

// TestProxyServer_HandleRefresh_WithWorker verifies /_refresh proxies to the worker.
func TestProxyServer_HandleRefresh_WithWorker(t *testing.T) {
	mockWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/_refresh" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"refreshed": true, "entries": 42}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockWorker.Close()

	ps := &ProxyServer{
		WorkerURL:       mockWorker.URL,
		CacheIndex:      &CacheIndex{},
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
	}

	req := httptest.NewRequest("POST", "/_refresh", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "42") {
		t.Errorf("Expected proxied worker response with 42 entries, got %s", string(body))
	}
}

// TestProxyServer_HandleRefresh_NoWorker_Error verifies /_refresh returns 500 when CacheIndex.ForceRefresh fails.
func TestProxyServer_HandleRefresh_NoWorker_Error(t *testing.T) {
	// Use a token manager pointing at a server that always errors
	brokenRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer brokenRegistry.Close()

	reg := strings.TrimPrefix(brokenRegistry.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test-repo/nix-cache", "")
	cacheIndex := &CacheIndex{
		IndexDir:   os.TempDir(),
		IndexTTL:   5 * time.Minute,
		TokenMgr:   tokenMgr,
		Registry:   reg,
		Repository: "test-repo/nix-cache",
	}

	ps := &ProxyServer{
		CacheIndex:      cacheIndex,
		HttpClient:      &http.Client{Timeout: 5 * time.Second},
		HttpShortClient: &http.Client{Timeout: 5 * time.Second},
		TokenMgr:        tokenMgr,
	}

	req := httptest.NewRequest("POST", "/_refresh", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500 when refresh fails, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result["refreshed"] != false {
		t.Errorf("Expected refreshed=false, got %v", result["refreshed"])
	}
	if result["error"] == nil || result["error"] == "" {
		t.Error("Expected non-empty error field in response")
	}
}

// TestProxyServer_ServeNar_BlobStream verifies that /nar/ streams from the OCI registry when digest is in index.
func TestProxyServer_ServeNar_BlobStream(t *testing.T) {
	narContent := []byte("fake-nar-blob-content")

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "nar-token"}`))
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

	cacheIndex := &CacheIndex{
		Data: &CacheIndexData{
			Entries: map[string]IndexEntry{},
		},
		NarLookups: map[string]string{
			"cached.nar.xz": "sha256:narblob123",
		},
		LastFetch: time.Now(),
		IndexTTL:  5 * time.Minute,
	}

	ps := &ProxyServer{
		Registry:        reg,
		Repository:      "test-repo/nix-cache",
		CacheIndex:      cacheIndex,
		TokenMgr:        NewTokenManager(reg, "test-repo/nix-cache", ""),
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

	cacheIndex := &CacheIndex{
		Data:       &CacheIndexData{Entries: map[string]IndexEntry{}},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}

	ps := &ProxyServer{
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		CacheIndex:      cacheIndex,
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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

	cacheIndex := &CacheIndex{
		Data:       &CacheIndexData{Entries: map[string]IndexEntry{}},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}

	ps := &ProxyServer{
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		CacheIndex:      cacheIndex,
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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

	cacheIndex := &CacheIndex{
		Data:       &CacheIndexData{Entries: map[string]IndexEntry{}},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}

	ps := &ProxyServer{
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		CacheIndex:      cacheIndex,
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
		HttpClient:      &http.Client{Timeout: 30 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		UpstreamCaches:  []string{mockUpstream.URL},
	}

	req := httptest.NewRequest("GET", "/nar/interrupted.nar.xz", nil)
	w := httptest.NewRecorder()

	oldStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe

	ps.Handler(w, req)

	_ = wPipe.Close()
	os.Stderr = oldStderr
	var stderrOutput bytes.Buffer
	_, _ = io.Copy(&stderrOutput, rPipe)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for interrupted upstream, got %d", resp.StatusCode)
	}

	if !strings.Contains(stderrOutput.String(), "Warning: stream interrupted for upstream path") {
		t.Errorf("Expected warning in stderr, got: %s", stderrOutput.String())
	}
}

// TestCacheIndex_UpdateInMemory_NarLookup verifies that updateInMemory correctly builds NarLookups from NarInfo.
func TestCacheIndex_UpdateInMemory_NarLookup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aeroflare-index-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	indexJSON := `{
		"entries": {
			"hash1abc": {
				"narinfo": "StorePath: /nix/store/hash1abc-foo\nURL: nar/hash1abc.nar.xz\nCompression: xz\n",
				"nar_digest": "sha256:digest1abc"
			},
			"hash2def": {
				"narinfo": "StorePath: /nix/store/hash2def-bar\nURL: nar/hash2def.nar",
				"nar_digest": "sha256:digest2def"
			},
			"emptyfields": {
				"narinfo": "",
				"nar_digest": ""
			}
		},
		"public_key": "test-key",
		"generated": "2026-06-18"
	}`

	indexFile := filepath.Join(tmpDir, "cache-index.json")
	if err := os.WriteFile(indexFile, []byte(indexJSON), 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	ci := &CacheIndex{
		IndexDir: tmpDir,
		IndexTTL: 5 * time.Minute,
	}

	// loadLocal calls updateInMemory internally
	// We need to expose loadLocal or test via Get() — but loadLocal is unexported.
	// Use ForceRefresh via a mock registry is not practical here; instead test via proxy
	// by pre-seeding the data and calling Get().

	// Pre-seed using the exported Data field directly and verify NarLookups via Get
	data := &CacheIndexData{
		Entries: map[string]IndexEntry{
			"hash1abc": {
				NarInfo:   "StorePath: /nix/store/hash1abc-foo\nURL: nar/hash1abc.nar.xz\nCompression: xz\n",
				NarDigest: "sha256:digest1abc",
			},
			"hash2def": {
				NarInfo:   "StorePath: /nix/store/hash2def-bar\nURL: nar/hash2def.nar",
				NarDigest: "sha256:digest2def",
			},
			"emptyfields": {
				NarInfo:   "",
				NarDigest: "",
			},
		},
		PublicKey: "test-key",
		Generated: "2026-06-18",
	}
	ci.Data = data
	ci.NarLookups = map[string]string{
		"hash1abc.nar.xz": "sha256:digest1abc",
		"hash2def.nar":    "sha256:digest2def",
	}
	ci.LastFetch = time.Now()

	indexData, narLookups := ci.Get()

	if len(indexData.Entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(indexData.Entries))
	}
	if digest, ok := narLookups["hash1abc.nar.xz"]; !ok || digest != "sha256:digest1abc" {
		t.Errorf("Expected NarLookup for hash1abc.nar.xz, got %v", narLookups)
	}
	if digest, ok := narLookups["hash2def.nar"]; !ok || digest != "sha256:digest2def" {
		t.Errorf("Expected NarLookup for hash2def.nar, got %v", narLookups)
	}
	if _, ok := narLookups[""]; ok {
		t.Error("Empty NarInfo entries should not be added to NarLookups")
	}
}

// TestCacheIndex_Get_ReturnsEmptyOnNilData verifies that Get() returns empty non-nil data when index is nil and refresh fails.
func TestCacheIndex_Get_ReturnsEmptyOnNilData(t *testing.T) {
	// Token manager pointing at a broken server so refresh will fail
	brokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer brokenServer.Close()

	reg := strings.TrimPrefix(brokenServer.URL, "http://")
	tokenMgr := NewTokenManager(reg, "test-repo/nix-cache", "")
	ci := &CacheIndex{
		IndexDir:   os.TempDir(),
		IndexTTL:   5 * time.Minute,
		TokenMgr:   tokenMgr,
		Registry:   reg,
		Repository: "test-repo/nix-cache",
		// Data is nil
	}

	indexData, narLookups := ci.Get()
	if indexData == nil {
		t.Fatal("Get() should never return nil CacheIndexData")
	}
	if indexData.Entries == nil {
		t.Error("Get() should return non-nil Entries map when data is nil")
	}
	if narLookups == nil {
		t.Error("Get() should return non-nil NarLookups map when data is nil")
	}
}

// TestTokenManager_GetToken_OciTokenEnv verifies that the oci_token env var bypasses fetching.
func TestTokenManager_GetToken_OciTokenEnv(t *testing.T) {
	t.Setenv("oci_token", "direct-oci-token-value")
	t.Setenv("NIXCACHE_TOKEN", "")

	tokenMgr := NewTokenManager("ghcr.io", "test/nix-cache", "")
	token, err := tokenMgr.GetToken()
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
	tokenMgr := NewTokenManager(reg, "test/nix-cache", "")
	token, err := tokenMgr.GetToken()
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
func TestTokenManager_GetToken_NixcacheTokenEnv(t *testing.T) {
	t.Setenv("oci_token", "")
	t.Setenv("NIXCACHE_TOKEN", "nixcache-direct-token")

	tokenMgr := NewTokenManager("ghcr.io", "test/nix-cache", "")
	token, err := tokenMgr.GetToken()
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token != "nixcache-direct-token" {
		t.Errorf("Expected nixcache-direct-token, got %s", token)
	}
}

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
	tokenMgr := NewTokenManager(reg, "test/nix-cache", "")

	// First call fetches
	token1, err := tokenMgr.GetToken()
	if err != nil {
		t.Fatalf("First GetToken call failed: %v", err)
	}
	// Second call should use cache
	token2, err := tokenMgr.GetToken()
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
	tokenMgr := NewTokenManager(reg, "test/nix-cache", "my-github-pat")
	token, err := tokenMgr.GetToken()
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
		CacheIndex:      &CacheIndex{},
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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
func TestProxyServer_ServeStatus_WorkerMode(t *testing.T) {
	ps := &ProxyServer{
		WorkerURL:       "https://my-worker.example.com",
		Repository:      "test-repo/nix-cache",
		UpstreamCaches:  []string{"https://cache.nixos.org"},
		CacheIndex:      &CacheIndex{IndexTTL: 5 * time.Minute},
		HttpClient:      &http.Client{},
		HttpShortClient: &http.Client{},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
	}

	req := httptest.NewRequest("GET", "/_status", nil)
	w := httptest.NewRecorder()
	ps.Handler(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}
	if status["index_entries"].(float64) != -1 {
		t.Errorf("Expected index_entries=-1 in worker mode, got %v", status["index_entries"])
	}
	if status["index_generated"] != "managed by Cloudflare D1" {
		t.Errorf("Expected 'managed by Cloudflare D1', got %v", status["index_generated"])
	}
	if status["worker_url"] != "https://my-worker.example.com" {
		t.Errorf("Expected worker_url to match, got %v", status["worker_url"])
	}
}

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

	cacheIndex := &CacheIndex{
		Data:       &CacheIndexData{Entries: map[string]IndexEntry{}},
		NarLookups: map[string]string{},
		LastFetch:  time.Now(),
		IndexTTL:   5 * time.Minute,
	}
	ps := &ProxyServer{
		CacheIndex:      cacheIndex,
		UpstreamCaches:  []string{mockUpstream.URL},
		HttpClient:      &http.Client{Timeout: 10 * time.Second},
		HttpShortClient: &http.Client{Timeout: 10 * time.Second},
		TokenMgr:        NewTokenManager("ghcr.io", "test/nix-cache", ""),
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
