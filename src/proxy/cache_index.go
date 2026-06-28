package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CacheIndex handles loading, refreshing, and querying the cache index.
//
// The cache-index OCI manifest is the single source of truth. Its annotations
// determine the mode via the "index-type" label:
//   - "r2": narinfo is served from public-r2-url; only the manifest (not the
//     large index blob) is fetched.
//   - "native": narinfo hashes are used as tags in the registry.
//   - "json" (or absent): the index blob is downloaded and cached locally so
//     narinfo and NAR lookups can be served from memory.
type CacheIndex struct {
	mu                  sync.RWMutex
	refreshMu           sync.Mutex
	Data                *CacheIndexData
	NarLookups          map[string]string // narBasename -> narDigest (json mode only)
	ManifestAnnotations map[string]string
	LastFetch           time.Time
	IndexDir            string
	CacheFileName       string // local cache file name (json mode)
	IndexTTL            time.Duration
	TokenMgr            *TokenManager
	Registry            string
	Repository          string
}

func (ci *CacheIndex) IndexType() string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	if ci.ManifestAnnotations == nil {
		return "json"
	}
	if t := ci.ManifestAnnotations["aeroflare.index-type"]; t != "" {
		return t
	}
	if t := ci.ManifestAnnotations["index-type"]; t != "" {
		return t
	}
	return "json"
}

// IsR2 reports whether the cache is configured to serve narinfo from R2.
func (ci *CacheIndex) IsR2() bool {
	return ci.IndexType() == "r2"
}

// PublicR2URL returns the configured public R2 URL for narinfo, or "" if none.
func (ci *CacheIndex) PublicR2URL() string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	if ci.ManifestAnnotations == nil {
		return ""
	}
	if u := ci.ManifestAnnotations["public-r2-url"]; u != "" {
		return u
	}
	return ci.ManifestAnnotations["aeroflare.r2.public_url"]
}

// Get returns the current cache index data and NAR lookups map, triggering a TTL-based refresh if needed.
func (ci *CacheIndex) Get(ctx context.Context) (*CacheIndexData, map[string]string) {
	ci.mu.RLock()
	needRefresh := time.Since(ci.LastFetch) > ci.IndexTTL
	isNil := ci.Data == nil && ci.ManifestAnnotations == nil
	ci.mu.RUnlock()

	if isNil {
		ci.refreshMu.Lock()
		ci.mu.RLock()
		isNil = ci.Data == nil && ci.ManifestAnnotations == nil
		ci.mu.RUnlock()
		if isNil {
			if err := ci.refresh(ctx); err != nil {
				slog.Warn("Failed to refresh index. Using cached index.", "error", err)
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
			// context.WithoutCancel (Go 1.21+) ensures the background refresh isn't aborted
			// if the HTTP request triggering it is completed/closed by the client.
			bgCtx := context.WithoutCancel(ctx)
			go func() {
				defer ci.refreshMu.Unlock()
				if err := ci.refresh(bgCtx); err != nil {
					slog.Warn("Failed to refresh index in background", "error", err)
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
func (ci *CacheIndex) ForceRefresh(ctx context.Context) (int, error) {
	ci.refreshMu.Lock()
	defer ci.refreshMu.Unlock()

	err := ci.refresh(ctx)
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
	if ci.CacheFileName == "" {
		ci.CacheFileName = "cache-index.json"
	}
	indexFile := filepath.Join(ci.IndexDir, ci.CacheFileName)
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

func (ci *CacheIndex) refresh(ctx context.Context) error {
	token, err := ci.TokenMgr.GetToken(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	proto := GetProtocol(ci.Registry)

	// 1. Fetch manifest — this is the single source of truth for annotations.
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-index", proto, ci.Registry, ci.Repository)
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return fmt.Errorf("failed creating manifest request: %w", err)
	}
	req.Header.Set("Accept", ociManifestMediaType) // Konstante aus bootstrap.go
	req.Header.Set("User-Agent", userAgent)        // Konstante aus bootstrap.go
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed fetching manifest from %s: %w", manifestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("manifest HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var manifest IndexManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed decoding manifest JSON: %w", err)
	}

	ci.mu.Lock()
	ci.ManifestAnnotations = manifest.Annotations
	ci.mu.Unlock()

	indexType := ci.IndexType()

	// In r2 and native modes the index blob is not needed.
	if indexType == "r2" || indexType == "native" {
		ci.mu.Lock()
		// Clear any stale in-memory index from a previous json-mode run.
		ci.Data = &CacheIndexData{Entries: make(map[string]IndexEntry)}
		ci.NarLookups = make(map[string]string)
		ci.mu.Unlock()
		if indexType == "r2" {
			slog.Info("Manifest refreshed", "mode", indexType, "public_r2_url", ci.PublicR2URL())
		} else {
			slog.Info("Manifest refreshed", "mode", indexType)
		}
		return nil
	}

	if len(manifest.Layers) == 0 {
		return fmt.Errorf("no layers in cache-index manifest")
	}

	digest := manifest.Layers[0].Digest

	// 2. Fetch blob (json mode)
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ci.Registry, ci.Repository, digest)
	req, err = http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return fmt.Errorf("failed creating blob request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	blobClient := &http.Client{Timeout: 120 * time.Second}
	resp, err = blobClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed fetching blob from %s: %w", blobURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("blob HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	dataBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed reading blob response body: %w", err)
	}

	var indexData CacheIndexData
	if err := json.Unmarshal(dataBytes, &indexData); err != nil {
		return fmt.Errorf("failed unmarshaling blob JSON: %w", err)
	}

	if err := os.MkdirAll(ci.IndexDir, 0755); err == nil {
		if ci.CacheFileName == "" {
			ci.CacheFileName = "cache-index.json"
		}
		indexFile := filepath.Join(ci.IndexDir, ci.CacheFileName)
		_ = os.WriteFile(indexFile, dataBytes, 0644)
	}

	ci.updateInMemory(&indexData)
	slog.Info("Index refreshed", "entries", len(indexData.Entries))
	return nil
}
