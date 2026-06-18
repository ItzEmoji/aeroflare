package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aeroflare/src"
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
	cacheIndex := &network.CacheIndex{
		Data: &network.CacheIndexData{
			PublicKey: "test-public-key",
			Generated: "2026-06-18",
			Entries: map[string]network.IndexEntry{
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

	ps := &network.ProxyServer{
		Port:            37515,
		ListenAddr:      "127.0.0.1",
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		UpstreamCaches:  []string{mockUpstream.URL},
		TokenMgr:        network.NewTokenManager("ghcr.io", "test-repo/nix-cache", ""),
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
	bodyBytes, _ := io.ReadAll(resp.Body)
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

	ps := &network.ProxyServer{
		Port:            37515,
		ListenAddr:      "127.0.0.1",
		Registry:        "ghcr.io",
		Repository:      "test-repo/nix-cache",
		UpstreamCaches:  []string{"https://cache.nixos.org"},
		TokenMgr:        network.NewTokenManager("ghcr.io", "test-repo/nix-cache", ""),
		CacheIndex:      &network.CacheIndex{},
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

	tokenMgr := network.NewTokenManager(u, "test-repo/nix-cache", "")
	conf, err := network.BootstrapConfig(u, "test-repo/nix-cache", tokenMgr)
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
