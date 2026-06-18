package network

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// IndexEntry holds the narinfo text and the OCI blob digest for a single Nix store path.
type IndexEntry struct {
	NarInfo   string `json:"narinfo"`
	NarDigest string `json:"nar_digest"`
}

// CacheIndexData is the deserialized content of the cache-index OCI manifest layer.
type CacheIndexData struct {
	Entries   map[string]IndexEntry `json:"entries"`
	PublicKey string                `json:"public_key"`
	Generated string                `json:"generated"`
}

// CacheIndex holds the in-memory index fetched from the OCI registry and provides
// thread-safe access via Get and ForceRefresh.
type CacheIndex struct {
	Data       *CacheIndexData
	NarLookups map[string]string // narFilename → blobDigest
	LastFetch  time.Time
	IndexTTL   time.Duration
	IndexDir   string
	TokenMgr   *TokenManager
	Registry   string
	Repository string
	mu         sync.RWMutex
}

// Get returns the current CacheIndexData and NarLookups, triggering a refresh when the
// data is nil or the TTL has expired. It always returns non-nil maps.
func (ci *CacheIndex) Get() (*CacheIndexData, map[string]string) {
	ci.mu.RLock()
	data := ci.Data
	narLookups := ci.NarLookups
	stale := data == nil || (!ci.LastFetch.IsZero() && time.Since(ci.LastFetch) > ci.IndexTTL) || (ci.LastFetch.IsZero() && data == nil)
	ci.mu.RUnlock()

	if stale {
		_ = ci.ForceRefresh()
		ci.mu.RLock()
		data = ci.Data
		narLookups = ci.NarLookups
		ci.mu.RUnlock()
	}

	if data == nil {
		data = &CacheIndexData{Entries: map[string]IndexEntry{}}
	}
	if narLookups == nil {
		narLookups = map[string]string{}
	}
	return data, narLookups
}

// ForceRefresh fetches the cache-index manifest and blob from the OCI registry and
// updates the in-memory data.
func (ci *CacheIndex) ForceRefresh() error {
	if ci.TokenMgr == nil || ci.Registry == "" {
		return fmt.Errorf("CacheIndex: TokenMgr or Registry not configured")
	}

	token, err := ci.TokenMgr.GetToken()
	if err != nil {
		return fmt.Errorf("CacheIndex: failed to get token: %w", err)
	}

	proto := GetProtocol(ci.Registry)
	client := &http.Client{Timeout: 30 * time.Second}

	// Fetch cache-index manifest
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-index", proto, ci.Registry, ci.Repository)
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("CacheIndex: manifest fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CacheIndex: manifest fetch HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("CacheIndex: failed to parse manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return fmt.Errorf("CacheIndex: manifest has no layers")
	}

	blobDigest := manifest.Layers[0].Digest

	// Fetch blob
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ci.Registry, ci.Repository, blobDigest)
	req, err = http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	blobResp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("CacheIndex: blob fetch failed: %w", err)
	}
	defer func() { _ = blobResp.Body.Close() }()

	if blobResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(blobResp.Body)
		return fmt.Errorf("CacheIndex: blob fetch HTTP %d: %s", blobResp.StatusCode, string(bodyBytes))
	}

	var newData CacheIndexData
	if err := json.NewDecoder(blobResp.Body).Decode(&newData); err != nil {
		return fmt.Errorf("CacheIndex: failed to parse index blob: %w", err)
	}
	if newData.Entries == nil {
		newData.Entries = map[string]IndexEntry{}
	}

	// Build NarLookups from NarInfo text
	newNarLookups := make(map[string]string, len(newData.Entries))
	for _, entry := range newData.Entries {
		if entry.NarInfo == "" || entry.NarDigest == "" {
			continue
		}
		for _, line := range strings.Split(entry.NarInfo, "\n") {
			if strings.HasPrefix(line, "URL: nar/") {
				narFile := strings.TrimPrefix(line, "URL: nar/")
				narFile = strings.TrimSpace(narFile)
				if narFile != "" {
					newNarLookups[narFile] = entry.NarDigest
				}
				break
			}
		}
	}

	ci.mu.Lock()
	ci.Data = &newData
	ci.NarLookups = newNarLookups
	ci.LastFetch = time.Now()
	ci.mu.Unlock()

	return nil
}

// TokenManager handles OCI registry authentication, caching bearer tokens.
type TokenManager struct {
	registry    string
	repository  string
	githubToken string
	cachedToken string
	mu          sync.Mutex
}

// NewTokenManager creates a new TokenManager for the given registry, repository, and optional GitHub PAT.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
	return &TokenManager{
		registry:    registry,
		repository:  repository,
		githubToken: githubToken,
	}
}

// GetToken returns a valid bearer token, using environment variables or token exchange as needed.
func (tm *TokenManager) GetToken() (string, error) {
	ociToken := os.Getenv("oci_token")
	if ociToken != "" && !strings.HasPrefix(ociToken, "ghp_") && !strings.HasPrefix(ociToken, "github_pat_") {
		return ociToken, nil
	}

	nixcacheToken := os.Getenv("NIXCACHE_TOKEN")
	if nixcacheToken != "" {
		return nixcacheToken, nil
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.cachedToken != "" {
		return tm.cachedToken, nil
	}

	// Determine basic auth token for exchange
	basicAuth := tm.githubToken
	if basicAuth == "" && (strings.HasPrefix(ociToken, "ghp_") || strings.HasPrefix(ociToken, "github_pat_")) {
		basicAuth = ociToken
	}

	token, err := ExchangeToken(tm.registry, tm.repository, basicAuth)
	if err != nil {
		return "", err
	}

	tm.cachedToken = token
	return token, nil
}

// ServerConfig holds the bootstrapped configuration fetched from the OCI registry.
type ServerConfig struct {
	WorkerURL      string   `json:"worker_url"`
	PublicKey      string   `json:"public_key"`
	UpstreamCaches []string `json:"upstream_caches"`
}

// BootstrapConfig fetches the cache-config manifest and its blob from the OCI registry,
// returning a ServerConfig with the decoded configuration.
func BootstrapConfig(registry, repository string, tokenMgr *TokenManager) (*ServerConfig, error) {
	token, err := tokenMgr.GetToken()
	if err != nil {
		return nil, fmt.Errorf("BootstrapConfig: failed to get token: %w", err)
	}

	proto := GetProtocol(registry)
	client := &http.Client{Timeout: 30 * time.Second}

	// Fetch cache-config manifest
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-config", proto, registry, repository)
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BootstrapConfig: manifest fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("BootstrapConfig: manifest fetch HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("BootstrapConfig: failed to parse manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("BootstrapConfig: manifest has no layers")
	}

	blobDigest := manifest.Layers[0].Digest

	// Fetch config blob
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, blobDigest)
	req, err = http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	blobResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BootstrapConfig: blob fetch failed: %w", err)
	}
	defer func() { _ = blobResp.Body.Close() }()

	if blobResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(blobResp.Body)
		return nil, fmt.Errorf("BootstrapConfig: blob fetch HTTP %d: %s", blobResp.StatusCode, string(bodyBytes))
	}

	var cfg ServerConfig
	if err := json.NewDecoder(blobResp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("BootstrapConfig: failed to parse config blob: %w", err)
	}

	return &cfg, nil
}

// ProxyServer is an HTTP server that serves a Nix binary cache backed by an OCI registry.
type ProxyServer struct {
	Port            int
	ListenAddr      string
	Registry        string
	Repository      string
	UpstreamCaches  []string
	TokenMgr        *TokenManager
	CacheIndex      *CacheIndex
	HttpClient      *http.Client
	HttpShortClient *http.Client
	WorkerURL       string
	PublicKey       string
}

// Handler is the HTTP handler for the proxy server.
func (ps *ProxyServer) Handler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		ps.handleGet(w, r)
	case http.MethodPost:
		ps.handlePost(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (ps *ProxyServer) handleGet(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/nix-cache-info":
		w.Header().Set("Content-Type", "text/x-nix-cache-info")
		_, _ = fmt.Fprint(w, "StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n")

	case path == "/public-key":
		ps.servePublicKey(w, r)

	case path == "/_status":
		ps.serveStatus(w, r)

	case strings.HasSuffix(path, ".narinfo"):
		ps.serveNarInfo(w, r)

	case strings.HasPrefix(path, "/nar/"):
		ps.serveNar(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (ps *ProxyServer) handlePost(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_refresh":
		ps.handleRefresh(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (ps *ProxyServer) servePublicKey(w http.ResponseWriter, r *http.Request) {
	// Check server-level field first
	if key := strings.TrimSpace(ps.PublicKey); key != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "%s\n", key)
		return
	}

	// Check local cache index
	data, _ := ps.CacheIndex.Get()
	if key := strings.TrimSpace(data.PublicKey); key != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "%s\n", key)
		return
	}

	// Proxy to worker if configured
	if ps.WorkerURL != "" {
		ps.proxyToWorker(w, r)
		return
	}

	http.NotFound(w, r)
}

func (ps *ProxyServer) serveStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{}

	if ps.WorkerURL != "" {
		status["index_entries"] = -1
		status["index_generated"] = "managed by Cloudflare D1"
		status["worker_url"] = ps.WorkerURL
	} else {
		data, _ := ps.CacheIndex.Get()
		status["index_entries"] = len(data.Entries)
		status["index_generated"] = data.Generated
	}

	status["repository"] = ps.Repository
	status["upstream_caches"] = ps.UpstreamCaches

	ttl := ""
	if ps.CacheIndex != nil {
		ttl = ps.CacheIndex.IndexTTL.String()
	}
	status["index_ttl"] = ttl

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (ps *ProxyServer) serveNarInfo(w http.ResponseWriter, r *http.Request) {
	// Extract hash from path like /{hash}.narinfo
	base := strings.TrimPrefix(r.URL.Path, "/")
	hash := strings.TrimSuffix(base, ".narinfo")

	// Check local index
	data, _ := ps.CacheIndex.Get()
	if entry, ok := data.Entries[hash]; ok {
		w.Header().Set("Content-Type", "text/x-nix-narinfo")
		_, _ = fmt.Fprint(w, entry.NarInfo)
		return
	}

	// Try worker if configured
	if ps.WorkerURL != "" {
		ps.proxyToWorker(w, r)
		return
	}

	// Fall back to upstream caches
	for _, upstream := range ps.UpstreamCaches {
		upstreamURL := strings.TrimRight(upstream, "/") + r.URL.Path
		resp, err := ps.HttpShortClient.Get(upstreamURL)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			w.Header().Set("Content-Type", "text/x-nix-narinfo")
			_, _ = io.Copy(w, resp.Body)
			_ = resp.Body.Close()
			return
		}
		_ = resp.Body.Close()
	}

	http.NotFound(w, r)
}

func (ps *ProxyServer) serveNar(w http.ResponseWriter, r *http.Request) {
	narFile := strings.TrimPrefix(r.URL.Path, "/nar/")

	// Check NarLookups in the index
	_, narLookups := ps.CacheIndex.Get()
	if blobDigest, ok := narLookups[narFile]; ok {
		// Fetch blob from OCI registry
		token, err := ps.TokenMgr.GetToken()
		if err == nil {
			proto := GetProtocol(ps.Registry)
			blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, blobDigest)
			req, err := http.NewRequest("GET", blobURL, nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("User-Agent", "aeroflare/1.0")
				resp, err := ps.HttpClient.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					w.Header().Set("Content-Type", narContentType(narFile))
					if cl := resp.Header.Get("Content-Length"); cl != "" {
						w.Header().Set("Content-Length", cl)
					}
					_, _ = io.Copy(w, resp.Body)
					_ = resp.Body.Close()
					return
				}
				if resp != nil {
					_ = resp.Body.Close()
				}
			}
		}
	}

	// Try worker if configured
	if ps.WorkerURL != "" {
		ps.proxyToWorker(w, r)
		return
	}

	// Fall back to upstream caches
	for _, upstream := range ps.UpstreamCaches {
		upstreamURL := strings.TrimRight(upstream, "/") + r.URL.Path
		resp, err := ps.HttpClient.Get(upstreamURL)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			w.Header().Set("Content-Type", narContentType(narFile))
			_, _ = io.Copy(w, resp.Body)
			_ = resp.Body.Close()
			return
		}
		_ = resp.Body.Close()
	}

	http.NotFound(w, r)
}

func (ps *ProxyServer) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// Proxy to worker if configured
	if ps.WorkerURL != "" {
		ps.proxyToWorker(w, r)
		return
	}

	err := ps.CacheIndex.ForceRefresh()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"refreshed": false,
			"error":     err.Error(),
		})
		return
	}

	data, _ := ps.CacheIndex.Get()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"refreshed": true,
		"entries":   len(data.Entries),
	})
}

// proxyToWorker forwards the current request to the WorkerURL and relays the response.
func (ps *ProxyServer) proxyToWorker(w http.ResponseWriter, r *http.Request) {
	targetURL := strings.TrimRight(ps.WorkerURL, "/") + r.URL.RequestURI()
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "proxy error", http.StatusBadGateway)
		return
	}
	for key, vals := range r.Header {
		for _, v := range vals {
			req.Header.Add(key, v)
		}
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := ps.HttpShortClient.Do(req)
	if err != nil {
		http.Error(w, "proxy error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// narContentType returns an appropriate Content-Type for a NAR file based on its extension.
func narContentType(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".nar.xz"):
		return "application/x-xz"
	case strings.HasSuffix(filename, ".nar.zst"):
		return "application/zstd"
	default:
		return "application/x-nix-nar"
	}
}