package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Types for cache-index.json management
type PushCacheIndex struct {
	Version   int                       `json:"version"`
	Repo      string                    `json:"repo"`
	Registry  string                    `json:"registry"`
	Image     string                    `json:"image"`
	Generated string                    `json:"generated"`
	PublicKey string                    `json:"public_key"`
	Entries   map[string]PushCacheEntry `json:"entries"`
	GCRoots   []string                  `json:"gc_roots"`
}

type PushCacheEntry struct {
	Name      string `json:"name"`
	Narinfo   string `json:"narinfo"`
	NarDigest string `json:"nar_digest"`
	NarSize   int64  `json:"nar_size"`
	Added     string `json:"added"`
}

type PushReceipt struct {
	StorePath   string
	NarinfoPath string
	NarDigest   string
	NarSize     int64
	IsRoot      bool
}

// FetchCacheIndex fetches the current cache-index.
func FetchCacheIndex(registry, repository, token string) (*PushCacheIndex, error) {
	scheme := "https"
	if strings.HasPrefix(registry, "localhost:") || strings.HasPrefix(registry, "127.0.0.1:") {
		scheme = "http"
	}
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-index", scheme, registry, repository)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	var existingIndex PushCacheIndex
	existingIndex.Entries = make(map[string]PushCacheEntry)

	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		var manifest struct {
			Layers []struct {
				Digest string `json:"digest"`
			} `json:"layers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&manifest); err == nil && len(manifest.Layers) > 0 {
			tmpIndex, err := os.CreateTemp("", "existing-index-*.json")
			if err == nil {
				_ = tmpIndex.Close()
				defer func() { _ = os.Remove(tmpIndex.Name()) }()

				if err := PullBlob(manifest.Layers[0].Digest, tmpIndex.Name(), registry, repository, token); err == nil {
					if b, err := os.ReadFile(tmpIndex.Name()); err == nil {
						_ = json.Unmarshal(b, &existingIndex)
						if existingIndex.Entries == nil {
							existingIndex.Entries = make(map[string]PushCacheEntry)
						}
					}
				}
			}
		}
	} else if resp != nil {
		_ = resp.Body.Close()
	}

	return &existingIndex, nil
}

// UpdateCacheIndex merges new entries into existingIndex and pushes the updated manifest.
func UpdateCacheIndex(receipts []PushReceipt, existingIndex *PushCacheIndex, registry, repository, token, pubKeyPath string) error {


	scheme := "https"
	if strings.HasPrefix(registry, "localhost:") || strings.HasPrefix(registry, "127.0.0.1:") {
		scheme = "http"
	}
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-index", scheme, registry, repository)

	// Determine public key
	pubKey := existingIndex.PublicKey
	if pubKeyPath != "" {
		if b, err := os.ReadFile(pubKeyPath + ".pub"); err == nil {
			pubKey = strings.TrimSpace(string(b))
		}
	}

	imageName := os.Getenv("NIXCACHE_IMAGE")
	if imageName == "" {
		imageName = repository
	}

	// Build new index
	newIndex := PushCacheIndex{
		Version:   1,
		Repo:      repository,
		Registry:  registry,
		Image:     imageName,
		Generated: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		PublicKey: pubKey,
		Entries:   existingIndex.Entries,
		GCRoots:   existingIndex.GCRoots,
	}

	if newIndex.Entries == nil {
		newIndex.Entries = make(map[string]PushCacheEntry)
	}

	gcRootsSet := make(map[string]bool)
	for _, root := range newIndex.GCRoots {
		gcRootsSet[root] = true
	}

	newEntriesCount := 0
	for _, r := range receipts {
		b, err := os.ReadFile(r.NarinfoPath)
		if err != nil {
			continue
		}

		basename := filepath.Base(r.StorePath)
		parts := strings.SplitN(basename, "-", 2)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		name := parts[1]

		newIndex.Entries[hash] = PushCacheEntry{
			Name:      name,
			Narinfo:   string(b),
			NarDigest: r.NarDigest,
			NarSize:   r.NarSize,
			Added:     newIndex.Generated,
		}
		newEntriesCount++

		if r.IsRoot {
			if !gcRootsSet[hash] {
				newIndex.GCRoots = append(newIndex.GCRoots, hash)
				gcRootsSet[hash] = true
			}
		}
	}

	sort.Strings(newIndex.GCRoots)

	// Push new index JSON as blob
	outIndex, _ := os.CreateTemp("", "new-index-*.json")
	defer func() { _ = os.Remove(outIndex.Name()) }()

	b, _ := json.MarshalIndent(newIndex, "", "  ")
	_ = os.WriteFile(outIndex.Name(), b, 0644)

	indexDigest, err := PushBlob(outIndex.Name(), registry, repository, token)
	if err != nil {
		return fmt.Errorf("push cache index blob: %w", err)
	}
	indexStat, _ := os.Stat(outIndex.Name())

	// Push empty config JSON as blob
	outConfig, _ := os.CreateTemp("", "empty-config-*.json")
	defer func() { _ = os.Remove(outConfig.Name()) }()
	_ = os.WriteFile(outConfig.Name(), []byte("{}"), 0644)

	configDigest, err := PushBlob(outConfig.Name(), registry, repository, token)
	if err != nil {
		return fmt.Errorf("push config blob: %w", err)
	}
	configStat, _ := os.Stat(outConfig.Name())

	// Push Cache-Index Manifest
	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      configStat.Size(),
		},
		"layers": []map[string]interface{}{
			{
				"mediaType": "application/vnd.nix.cache.index.v1+json",
				"digest":    indexDigest,
				"size":      indexStat.Size(),
			},
		},
	}

	manifestBytes, _ := json.Marshal(manifest)
	client := &http.Client{Timeout: 30 * time.Second}
	putReq, err := http.NewRequest("PUT", manifestURL, bytes.NewReader(manifestBytes))
	if err != nil {
		return fmt.Errorf("create manifest put request: %w", err)
	}
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")

	putResp, err := client.Do(putReq)
	if err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}
	defer func() { _ = putResp.Body.Close() }()

	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("push manifest failed with HTTP %d: %s", putResp.StatusCode, string(respBytes))
	}


	return nil
}
