package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// GetProtocol determines http vs https protocol (useful for local mock registry tests)
func GetProtocol(registry string) string {
	if strings.HasPrefix(registry, "127.0.0.1") || strings.HasPrefix(registry, "localhost") {
		return "http"
	}
	return "https"
}

// IndexManifest represents the OCI image manifest for the cache index or configuration.
type IndexManifest struct {
	Layers []IndexLayer `json:"layers"`
}

// IndexLayer represents a layer in the OCI manifest.
type IndexLayer struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// IndexEntry represents a single cached store path entry.
type IndexEntry struct {
	NarInfo   string `json:"narinfo"`
	NarDigest string `json:"nar_digest"`
}

// CacheIndexData represents the parsed cache-index JSON payload.
type CacheIndexData struct {
	Entries   map[string]IndexEntry `json:"entries"`
	PublicKey string                `json:"public_key"`
	Generated string                `json:"generated"`
}

// RemoteConfig represents the optional dynamic configuration JSON loaded from GHCR.
type RemoteConfig struct {
	WorkerURL      string   `json:"worker_url"`
	PublicKey      string   `json:"public_key"`
	UpstreamCaches []string `json:"upstream_caches"`
}

// TokenManager handles retrieving and caching the OCI Bearer token.
type TokenManager struct {
	registry    string
	repository  string
	githubToken string
	mu          sync.Mutex
	token       string
	expiry      time.Time
}

// NewTokenManager creates a new OCI token manager.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
	return &TokenManager{
		registry:    registry,
		repository:  repository,
		githubToken: githubToken,
	}
}

// GetToken returns a valid OCI Bearer token, performing token exchange if necessary.
func (tm *TokenManager) GetToken() (string, error) {
	if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t, nil
	}
	if t := os.Getenv("NIXCACHE_TOKEN"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t, nil
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.token != "" && time.Now().Before(tm.expiry) {
		return tm.token, nil
	}

	token, err := tm.fetchToken()
	if err != nil {
		return "", err
	}

	tm.token = token
	tm.expiry = time.Now().Add(4 * time.Minute) // Cache for 4 minutes
	return tm.token, nil
}

func (tm *TokenManager) fetchToken() (string, error) {
	scope := fmt.Sprintf("repository:%s:pull", tm.repository)
	proto := GetProtocol(tm.registry)
	tokenURL := fmt.Sprintf("%s://%s/token?scope=%s&service=%s", proto, tm.registry, scope, tm.registry)

	client := &http.Client{Timeout: 10 * time.Second}

	if tm.githubToken != "" {
		req, err := http.NewRequest("GET", tokenURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "aeroflare/1.0")
			req.SetBasicAuth("token", tm.githubToken)

			resp, err := client.Do(req)
			if err == nil {
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode == http.StatusOK {
					var result struct {
						Token string `json:"token"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Token != "" {
						return result.Token, nil
					}
				}
			}
		}
	}

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch token (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("empty token returned from registry")
	}

	return result.Token, nil
}

// CacheIndex handles loading, refreshing, and querying the cache index.
type CacheIndex struct {
	mu         sync.RWMutex
	refreshMu  sync.Mutex
	Data       *CacheIndexData
	NarLookups map[string]string // narBasename -> narDigest
	LastFetch  time.Time
	IndexDir   string
	IndexTTL   time.Duration
	TokenMgr   *TokenManager
	Registry   string
	Repository string
}

// Get returns the current cache index data and NAR lookups map, triggering a TTL-based refresh if needed.
func (ci *CacheIndex) Get() (*CacheIndexData, map[string]string) {
	ci.mu.RLock()
	needRefresh := time.Since(ci.LastFetch) > ci.IndexTTL
	isNil := ci.Data == nil
	ci.mu.RUnlock()

	if isNil {
		ci.refreshMu.Lock()
		ci.mu.RLock()
		isNil = ci.Data == nil
		ci.mu.RUnlock()
		if isNil {
			if err := ci.refresh(); err != nil {
				fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to refresh index: %v. Using cached index.\n", err)
				if ci.Data == nil {
					_ = ci.loadLocal()
				}
			}
			ci.mu.Lock()
			ci.LastFetch = time.Now()
			ci.mu.Unlock()
		}
		ci.refreshMu.Unlock()
	} else if needRefresh {
		if ci.refreshMu.TryLock() {
			go func() {
				defer ci.refreshMu.Unlock()
				if err := ci.refresh(); err != nil {
					fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to refresh index: %v.\n", err)
				}
				ci.mu.Lock()
				ci.LastFetch = time.Now()
				ci.mu.Unlock()
			}()
		}
	}

	ci.mu.RLock()
	defer ci.mu.RUnlock()

	if ci.Data == nil {
		return &CacheIndexData{Entries: make(map[string]IndexEntry)}, make(map[string]string)
	}
	return ci.Data, ci.NarLookups
}

// ForceRefresh triggers an unconditional refresh of the cache index.
func (ci *CacheIndex) ForceRefresh() (int, error) {
	ci.refreshMu.Lock()
	defer ci.refreshMu.Unlock()

	err := ci.refresh()
	ci.mu.Lock()
	ci.LastFetch = time.Now()
	entries := 0
	if ci.Data != nil {
		entries = len(ci.Data.Entries)
	}
	ci.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return entries, nil
}

func (ci *CacheIndex) loadLocal() error {
	indexFile := filepath.Join(ci.IndexDir, "cache-index.json")
	dataBytes, err := os.ReadFile(indexFile)
	if err != nil {
		return err
	}
	var indexData CacheIndexData
	if err := json.Unmarshal(dataBytes, &indexData); err != nil {
		return err
	}
	ci.updateInMemory(&indexData)
	return nil
}

func (ci *CacheIndex) updateInMemory(data *CacheIndexData) {
	narLookups := make(map[string]string)
	if data.Entries != nil {
		for _, entry := range data.Entries {
			if entry.NarInfo == "" || entry.NarDigest == "" {
				continue
			}
			idx := strings.Index(entry.NarInfo, "URL: ")
			if idx != -1 {
				start := idx + 5
				end := strings.IndexByte(entry.NarInfo[start:], '\n')
				var urlVal string
				if end == -1 {
					urlVal = entry.NarInfo[start:]
				} else {
					urlVal = entry.NarInfo[start : start+end]
				}
				urlVal = strings.TrimSpace(urlVal)
				parts := strings.Split(urlVal, "/")
				if len(parts) > 0 {
					basename := parts[len(parts)-1]
					narLookups[basename] = entry.NarDigest
				}
			}
		}
	}
	ci.mu.Lock()
	ci.Data = data
	ci.NarLookups = narLookups
	ci.mu.Unlock()
}

func (ci *CacheIndex) refresh() error {
	token, err := ci.TokenMgr.GetToken()
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	proto := GetProtocol(ci.Registry)

	// 1. Fetch manifest
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-index", proto, ci.Registry, ci.Repository)
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("manifest HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var manifest IndexManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return err
	}

	if len(manifest.Layers) == 0 {
		return fmt.Errorf("no layers in cache-index manifest")
	}

	digest := manifest.Layers[0].Digest

	// 2. Fetch blob
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ci.Registry, ci.Repository, digest)
	req, err = http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	blobClient := &http.Client{Timeout: 120 * time.Second}
	resp, err = blobClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("blob HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	dataBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var indexData CacheIndexData
	if err := json.Unmarshal(dataBytes, &indexData); err != nil {
		return err
	}

	if err := os.MkdirAll(ci.IndexDir, 0755); err == nil {
		indexFile := filepath.Join(ci.IndexDir, "cache-index.json")
		_ = os.WriteFile(indexFile, dataBytes, 0644)
	}

	ci.updateInMemory(&indexData)
	fmt.Fprintf(os.Stderr, "[aeroflare proxy] Index refreshed: %d entries\n", len(indexData.Entries))
	return nil
}

// ProxyServer bridges the Nix binary cache protocol to GHCR and upstream caches.
type ProxyServer struct {
	Port            int
	ListenAddr      string
	Registry        string
	Repository      string
	UpstreamCaches  []string
	TokenMgr        *TokenManager
	CacheIndex      *CacheIndex
	WorkerURL       string
	PublicKey       string
	HttpClient      *http.Client
	HttpShortClient *http.Client
}

// Handler handles all incoming HTTP requests for the Nix binary cache proxy.
func (ps *ProxyServer) Handler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		switch {
		case path == "/nix-cache-info":
			ps.serveNixCacheInfo(w)
		case path == "/public-key":
			ps.servePublicKey(w)
		case path == "/_status":
			ps.serveStatus(w)
		case strings.HasSuffix(path, ".narinfo"):
			ps.serveNarInfo(w, path)
		case strings.HasPrefix(path, "/nar/"):
			ps.serveNar(w, path, r.Method)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	case http.MethodPost:
		switch path {
		case "/_refresh":
			ps.handleRefresh(w)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (ps *ProxyServer) serveNixCacheInfo(w http.ResponseWriter) {
	data := []byte("StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n")
	w.Header().Set("Content-Type", "text/x-nix-cache-info")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (ps *ProxyServer) servePublicKey(w http.ResponseWriter) {
	pubKey := ps.PublicKey

	if pubKey == "" && ps.WorkerURL != "" {
		data, err := ps.fetchWorkerBytes("/public-key")
		if err == nil {
			pubKey = string(data)
		} else {
			fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to fetch public key from worker: %v\n", err)
		}
	}

	if pubKey == "" {
		indexData, _ := ps.CacheIndex.Get()
		pubKey = indexData.PublicKey
	}

	if pubKey != "" {
		pubKey = strings.TrimSpace(pubKey) + "\n"
		data := []byte(pubKey)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	} else {
		http.Error(w, "No public key configured", http.StatusNotFound)
	}
}

func (ps *ProxyServer) serveStatus(w http.ResponseWriter) {
	var indexEntries int
	var indexGenerated string

	if ps.WorkerURL == "" {
		indexData, _ := ps.CacheIndex.Get()
		indexEntries = len(indexData.Entries)
		indexGenerated = indexData.Generated
	} else {
		indexEntries = -1
		indexGenerated = "managed by Cloudflare D1"
	}

	status := map[string]interface{}{
		"index_entries":   indexEntries,
		"index_generated": indexGenerated,
		"index_ttl":       int(ps.CacheIndex.IndexTTL.Seconds()),
		"repo":            ps.Repository,
		"upstream":        ps.UpstreamCaches,
		"worker_url":      ps.WorkerURL,
		"has_public_key":  ps.PublicKey != "",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (ps *ProxyServer) handleRefresh(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")

	if ps.WorkerURL != "" {
		url := fmt.Sprintf("%s/_refresh", strings.TrimSuffix(ps.WorkerURL, "/"))
		req, err := http.NewRequest("POST", url, nil)
		if err == nil {
			req.Header.Set("User-Agent", "aeroflare/1.0")
			resp, err := ps.HttpShortClient.Do(req)
			if err == nil {
				defer func() { _ = resp.Body.Close() }()
				w.WriteHeader(resp.StatusCode)
				_, _ = io.Copy(w, resp.Body)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"refreshed": true,
			"note":      "refresh request sent to worker",
		})
		return
	}

	count, err := ps.CacheIndex.ForceRefresh()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"refreshed": false,
			"error":     err.Error(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"refreshed": true,
		"entries":   count,
	})
}

func (ps *ProxyServer) serveNarInfo(w http.ResponseWriter, path string) {
	storeHash := strings.TrimPrefix(path, "/")
	storeHash = strings.TrimSuffix(storeHash, ".narinfo")

	if ps.WorkerURL != "" {
		data, err := ps.fetchWorkerBytes(path)
		if err == nil {
			w.Header().Set("Content-Type", "text/x-nix-narinfo")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
	} else {
		indexData, _ := ps.CacheIndex.Get()
		if entry, ok := indexData.Entries[storeHash]; ok && entry.NarInfo != "" {
			body := []byte(entry.NarInfo)
			w.Header().Set("Content-Type", "text/x-nix-narinfo")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}

	upstreamPath := fmt.Sprintf("/%s.narinfo", storeHash)
	data, err := ps.fetchUpstreamBytes(upstreamPath)
	if err == nil {
		w.Header().Set("Content-Type", "text/x-nix-narinfo")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	http.Error(w, "Narinfo Not Found", http.StatusNotFound)
}

func (ps *ProxyServer) serveNar(w http.ResponseWriter, path string, method string) {
	narBasename := strings.TrimPrefix(path, "/nar/")
	contentType := "application/x-nix-nar"
	if strings.HasSuffix(narBasename, ".xz") {
		contentType = "application/x-xz"
	}

	var digest string

	if ps.WorkerURL != "" {
		digestBytes, err := ps.fetchWorkerBytes(fmt.Sprintf("/nar/%s/digest", narBasename))
		if err == nil {
			digest = strings.TrimSpace(string(digestBytes))
		} else {
			digestBytes, err = ps.fetchWorkerBytes(fmt.Sprintf("/digest/%s", narBasename))
			if err == nil {
				digest = strings.TrimSpace(string(digestBytes))
			}
		}
	} else {
		_, narLookups := ps.CacheIndex.Get()
		if d, ok := narLookups[narBasename]; ok && d != "" {
			digest = d
		}
	}

	if digest != "" && strings.HasPrefix(digest, "sha256:") {
		err := ps.streamBlob(w, digest, contentType, method)
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to stream blob %s from GHCR: %v. Trying upstream...\n", digest, err)
	}

	err := ps.streamUpstream(w, path, contentType, method)
	if err == nil {
		return
	}

	http.Error(w, "NAR Not Found", http.StatusNotFound)
}

func (ps *ProxyServer) streamBlob(w http.ResponseWriter, digest string, contentType string, method string) error {
	token, err := ps.TokenMgr.GetToken()
	if err != nil {
		return err
	}

	proto := GetProtocol(ps.Registry)
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, digest)
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ps.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GHCR blob download HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	w.Header().Set("Content-Type", contentType)
	if resp.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for blob %s: %v\n", digest, err)
	}
	return nil
}

func (ps *ProxyServer) streamUpstream(w http.ResponseWriter, path string, contentType string, method string) error {
	for _, cacheURL := range ps.UpstreamCaches {
		url := fmt.Sprintf("%s%s", strings.TrimSuffix(cacheURL, "/"), path)
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "aeroflare/1.0")

		resp, err := ps.HttpClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer func() { _ = resp.Body.Close() }()
			w.Header().Set("Content-Type", contentType)
			if resp.ContentLength > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
			}
			w.WriteHeader(http.StatusOK)
			_, err = io.Copy(w, resp.Body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for upstream path %s: %v\n", path, err)
			}
			return nil
		}
		_ = resp.Body.Close()
	}

	return fmt.Errorf("not found in any upstream cache")
}

func (ps *ProxyServer) fetchUpstreamBytes(path string) ([]byte, error) {
	for _, cacheURL := range ps.UpstreamCaches {
		url := fmt.Sprintf("%s%s", strings.TrimSuffix(cacheURL, "/"), path)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "aeroflare/1.0")

		resp, err := ps.HttpShortClient.Do(req)
		if err != nil {
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusOK {
			return io.ReadAll(resp.Body)
		}
	}

	return nil, fmt.Errorf("not found upstream")
}

func (ps *ProxyServer) fetchWorkerBytes(path string) ([]byte, error) {
	if ps.WorkerURL == "" {
		return nil, fmt.Errorf("no worker URL configured")
	}
	url := fmt.Sprintf("%s%s", strings.TrimSuffix(ps.WorkerURL, "/"), path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := ps.HttpShortClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("worker returned HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// BootstrapConfig fetches configuration dynamically from the GHCR 'cache-config' OCI image/blob.
func BootstrapConfig(registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, error) {
	token, err := tokenMgr.GetToken()
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	proto := GetProtocol(registry)

	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-config", proto, registry, repository)
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config manifest HTTP %d", resp.StatusCode)
	}

	var manifest IndexManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers in cache-config manifest")
	}

	digest := manifest.Layers[0].Digest
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, digest)
	req, err = http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config blob HTTP %d", resp.StatusCode)
	}

	var config RemoteConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// StartProxy starts the proxy HTTP server on the configured address.
func StartProxy(port int, listenAddr string, registry string, repository string, indexDir string, indexTTLSeconds int, upstreams []string, githubToken string, workerURL string) error {
	tokenMgr := NewTokenManager(registry, repository, githubToken)

	var bootstrappedKey string

	// Try bootstrapping configuration dynamically from GHCR config blob
	remoteConf, err := BootstrapConfig(registry, repository, tokenMgr)
	if err == nil && remoteConf != nil {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Dynamically bootstrapped configuration from GHCR 'cache-config' blob\n")
		if workerURL == "" && remoteConf.WorkerURL != "" {
			workerURL = remoteConf.WorkerURL
			fmt.Fprintf(os.Stderr, "  Worker URL: %s\n", workerURL)
		}
		if len(upstreams) == 1 && upstreams[0] == "https://cache.nixos.org" && len(remoteConf.UpstreamCaches) > 0 {
			upstreams = remoteConf.UpstreamCaches
			fmt.Fprintf(os.Stderr, "  Upstream Caches: %s\n", strings.Join(upstreams, ", "))
		}
		if remoteConf.PublicKey != "" {
			bootstrappedKey = remoteConf.PublicKey
			fmt.Fprintf(os.Stderr, "  Public Key: configured from remote config\n")
		}
	} else {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Note: could not bootstrap remote config from GHCR: %v (using local/env settings)\n", err)
	}

	ttl := time.Duration(indexTTLSeconds) * time.Second
	cacheIndex := &CacheIndex{
		IndexDir:   indexDir,
		IndexTTL:   ttl,
		TokenMgr:   tokenMgr,
		Registry:   registry,
		Repository: repository,
	}

	if workerURL == "" {
		_ = cacheIndex.loadLocal()
		go func() {
			_, _ = cacheIndex.Get()
		}()
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	ps := &ProxyServer{
		Port:           port,
		ListenAddr:     listenAddr,
		Registry:       registry,
		Repository:     repository,
		UpstreamCaches: upstreams,
		TokenMgr:       tokenMgr,
		CacheIndex:     cacheIndex,
		WorkerURL:      workerURL,
		PublicKey:      bootstrappedKey,
		HttpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Minute,
		},
		HttpShortClient: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ps.Handler)

	addr := fmt.Sprintf("%s:%d", listenAddr, port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Starting proxy server on http://%s\n", addr)
		fmt.Fprintf(os.Stderr, "  Repo: %s\n", repository)
		fmt.Fprintf(os.Stderr, "  Upstream: %s\n", strings.Join(upstreams, ", "))
		if workerURL != "" {
			fmt.Fprintf(os.Stderr, "  Backend Worker: %s (D1 database mapping mode)\n", workerURL)
		} else {
			fmt.Fprintf(os.Stderr, "  Index TTL: %s\n", ttl)
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-stop
	fmt.Fprintf(os.Stderr, "\n[aeroflare proxy] Shutting down...\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
