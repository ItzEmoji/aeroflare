package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ProxyServer bridges the Nix binary cache protocol to GHCR and upstream caches.
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
			ps.servePublicKey(w, r)
		case path == "/api/public-key":
			ps.serveApiPublicKey(w)
		case path == "/_status":
			ps.serveStatus(w, r)
		case strings.HasSuffix(path, ".narinfo"):
			ps.serveNarInfo(w, r, path)
		case strings.HasPrefix(path, "/nar/"):
			ps.serveNar(w, r, path, r.Method)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	case http.MethodPost:
		switch path {
		case "/_refresh":
			ps.handleRefresh(w, r)
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

func (ps *ProxyServer) servePublicKey(w http.ResponseWriter, r *http.Request) {
	// Primary source: the cache-index manifest annotation.
	ps.CacheIndex.mu.RLock()
	pubKey := ""
	if ps.CacheIndex.ManifestAnnotations != nil {
		pubKey = ps.CacheIndex.ManifestAnnotations["aeroflare.public-key"]
		if pubKey == "" {
			pubKey = ps.CacheIndex.ManifestAnnotations["public-key"]
		}
	}
	ps.CacheIndex.mu.RUnlock()

	// Fallback (json mode): the public_key field of the cached index blob.
	if pubKey == "" && !ps.CacheIndex.IsR2() {
		indexData, _ := ps.CacheIndex.Get(r.Context())
		if indexData != nil {
			pubKey = indexData.PublicKey
		}
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

func (ps *ProxyServer) serveApiPublicKey(w http.ResponseWriter) {
	ps.CacheIndex.mu.RLock()
	annotations := ps.CacheIndex.ManifestAnnotations
	ps.CacheIndex.mu.RUnlock()

	pubKey := ""
	if annotations != nil {
		pubKey = annotations["aeroflare.public-key"]
		if pubKey == "" {
			pubKey = annotations["public-key"]
		}
	}
	if pubKey != "" {
		pubKey = strings.TrimSpace(pubKey) + "\n"
		data := []byte(pubKey)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	} else {
		http.Error(w, "No public key configured in manifest", http.StatusNotFound)
	}
}

func (ps *ProxyServer) serveStatus(w http.ResponseWriter, r *http.Request) {
	indexData, _ := ps.CacheIndex.Get(r.Context())

	entriesCount := 0
	var generated string
	if indexData != nil {
		entriesCount = len(indexData.Entries)
		generated = indexData.Generated
	}

	status := map[string]interface{}{
		"index_entries":   entriesCount,
		"index_generated": generated,
		"index_ttl":       int(ps.CacheIndex.IndexTTL.Seconds()),
		"repo":            ps.Repository,
		"upstream":        ps.UpstreamCaches,
	}

	ps.CacheIndex.mu.RLock()
	annotations := ps.CacheIndex.ManifestAnnotations
	ps.CacheIndex.mu.RUnlock()

	indexType := ps.CacheIndex.IndexType()
	status["index_type"] = indexType

	if indexType == "r2" {
		publicURL := annotations["public-r2-url"]
		if publicURL == "" {
			publicURL = annotations["aeroflare.r2.public_url"]
		}
		status["r2_enabled"] = true
		status["r2_public_url"] = publicURL
	} else {
		status["r2_enabled"] = false
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (ps *ProxyServer) handleRefresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	count, err := ps.CacheIndex.ForceRefresh(r.Context())
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

func (ps *ProxyServer) serveNarInfo(w http.ResponseWriter, r *http.Request, path string) {
	storeHash := strings.TrimPrefix(path, "/")
	storeHash = strings.TrimSuffix(storeHash, ".narinfo")

	switch ps.CacheIndex.IndexType() {
	case "r2":
		// r2 mode: redirect the client to the public R2 URL.
		publicURL := ps.CacheIndex.PublicR2URL()
		if publicURL == "" {
			http.Error(w, "Error: The cache uses R2 but no public-url is configured. It is not possible to access this cache.", http.StatusForbidden)
			return
		}
		redirectURL := fmt.Sprintf("%s/%s.narinfo", strings.TrimRight(publicURL, "/"), storeHash)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	case "native":
		// native mode: pull narinfo from native OCI manifest
		if err := ps.serveNativeNarinfo(w, r, storeHash); err == nil {
			return
		}
	case "json":
		// json mode: serve from the cached index.
		indexData, _ := ps.CacheIndex.Get(r.Context())
		if indexData != nil {
			if entry, ok := indexData.Entries[storeHash]; ok && entry.NarInfo != "" {
				body := []byte(entry.NarInfo)
				w.Header().Set("Content-Type", "text/x-nix-narinfo")
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
				return
			}
		}
	}

	// Fallback to upstream cache.
	upstreamPath := fmt.Sprintf("/%s.narinfo", storeHash)
	if ps.proxyUpstream(w, r, upstreamPath) {
		return
	}

	http.Error(w, "Narinfo Not Found", http.StatusNotFound)
}

func (ps *ProxyServer) serveNar(w http.ResponseWriter, r *http.Request, path string, method string) {
	narBasename := strings.TrimPrefix(path, "/nar/")
	contentType := "application/x-nix-nar"
	if strings.HasSuffix(narBasename, ".xz") {
		contentType = "application/x-xz"
	}

	var digest string

	switch ps.CacheIndex.IndexType() {
	case "r2":
		// r2 mode: derive the blob digest from the narinfo's FileHash served by R2.
		if d, err := ps.digestFromR2Narinfo(r.Context(), narBasename); err == nil && d != "" {
			digest = d
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to derive digest from R2 narinfo for %s: %v. Trying upstream...\n", narBasename, err)
		}
	case "native":
		// native mode: pull the manifest and use the layer digest
		if d, err := ps.digestFromNativeManifest(r.Context(), narBasename); err == nil && d != "" {
			digest = d
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to derive digest from native manifest for %s: %v. Trying upstream...\n", narBasename, err)
		}
	case "json":
		// json mode: use the NAR lookup built from the cached index.
		_, narLookups := ps.CacheIndex.Get(r.Context())
		if narLookups != nil {
			if d, ok := narLookups[narBasename]; ok && d != "" {
				digest = d
			}
		}
	}

	if digest != "" && strings.HasPrefix(digest, "sha256:") {
		err := ps.streamBlob(r.Context(), w, digest, contentType, method)
		if err == nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to stream blob %s from GHCR: %v. Trying upstream...\n", digest, err)
	}

	if ps.proxyUpstream(w, r, path) {
		return
	}

	http.Error(w, "NAR Not Found", http.StatusNotFound)
}

func (ps *ProxyServer) serveNativeNarinfo(w http.ResponseWriter, r *http.Request, storeHash string) error {
	proto := GetProtocol(ps.Registry)
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", proto, ps.Registry, ps.Repository, storeHash)
	req, err := http.NewRequestWithContext(r.Context(), "GET", manifestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	token, _ := ps.TokenMgr.GetToken(r.Context())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ps.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var manifest struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
		Config      struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return err
	}

	var labels map[string]string

	if manifest.Annotations != nil && manifest.Annotations["vnd.aeroflare.nar.storepath"] != "" {
		labels = manifest.Annotations
	} else if manifest.Labels != nil && manifest.Labels["vnd.aeroflare.nar.storepath"] != "" {
		labels = manifest.Labels
	} else if manifest.Config.Digest != "" {
		configLabels, err := ps.fetchConfigLabels(r.Context(), manifest.Config.Digest)
		if err == nil && configLabels != nil {
			labels = configLabels
		}
	}

	if labels == nil {
		return fmt.Errorf("no narinfo labels found in manifest or config")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "StorePath: %s\n", labels["vnd.aeroflare.nar.storepath"])
	fmt.Fprintf(&b, "URL: %s\n", labels["vnd.aeroflare.nar.url"])
	fmt.Fprintf(&b, "Compression: %s\n", labels["vnd.aeroflare.nar.compression"])
	fmt.Fprintf(&b, "FileHash: %s\n", labels["vnd.aeroflare.nar.filehash"])
	fmt.Fprintf(&b, "FileSize: %s\n", labels["vnd.aeroflare.nar.filesize"])
	fmt.Fprintf(&b, "NarHash: %s\n", labels["vnd.aeroflare.nar.narhash"])
	fmt.Fprintf(&b, "NarSize: %s\n", labels["vnd.aeroflare.nar.narsize"])

	if rStr, ok := labels["vnd.aeroflare.nar.references"]; ok && rStr != "" {
		fmt.Fprintf(&b, "References: %s\n", rStr)
	} else {
		b.WriteString("References:\n")
	}

	if deriver, ok := labels["vnd.aeroflare.nar.deriver"]; ok && deriver != "" {
		fmt.Fprintf(&b, "Deriver: %s\n", deriver)
	} else {
		b.WriteString("Deriver:\n")
	}

	if system, ok := labels["vnd.aeroflare.nar.system"]; ok && system != "" {
		fmt.Fprintf(&b, "System: %s\n", system)
	}

	if sig, ok := labels["vnd.aeroflare.nar.sig"]; ok && sig != "" {
		fmt.Fprintf(&b, "Sig: %s\n", sig)
	}

	body := []byte(b.String())
	w.Header().Set("Content-Type", "text/x-nix-narinfo")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return nil
}

func (ps *ProxyServer) digestFromNativeManifest(ctx context.Context, narBasename string) (string, error) {
	tag := narBasename
	if idx := strings.Index(tag, ".nar"); idx != -1 {
		tag = tag[:idx]
	}

	proto := GetProtocol(ps.Registry)
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", proto, ps.Registry, ps.Repository, tag)
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	token, _ := ps.TokenMgr.GetToken(ctx)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ps.HttpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return "", err
	}
	if len(manifest.Layers) == 0 {
		return "", fmt.Errorf("no layers in manifest")
	}
	return manifest.Layers[0].Digest, nil
}

func (ps *ProxyServer) fetchConfigLabels(ctx context.Context, configDigest string) (map[string]string, error) {
	proto := GetProtocol(ps.Registry)
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, configDigest)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	token, _ := ps.TokenMgr.GetToken(ctx)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ps.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config blob HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var configData struct {
		Config struct {
			Labels      map[string]string `json:"Labels"`
			LabelsLower map[string]string `json:"labels"`
		} `json:"config"`
		Labels      map[string]string `json:"Labels"`
		LabelsLower map[string]string `json:"labels"`
	}

	if err := json.Unmarshal(body, &configData); err != nil {
		return nil, err
	}

	if len(configData.Config.Labels) > 0 {
		return configData.Config.Labels, nil
	}
	if len(configData.Labels) > 0 {
		return configData.Labels, nil
	}
	if len(configData.Config.LabelsLower) > 0 {
		return configData.Config.LabelsLower, nil
	}
	if len(configData.LabelsLower) > 0 {
		return configData.LabelsLower, nil
	}

	return nil, nil
}

// digestFromR2Narinfo fetches the narinfo for a NAR from the public R2 URL and
// converts its FileHash (sha256:<nix-base32> of the compressed NAR) into the
// GHCR blob digest form (sha256:<hex>).
func (ps *ProxyServer) digestFromR2Narinfo(ctx context.Context, narBasename string) (string, error) {
	publicURL := ps.CacheIndex.PublicR2URL()
	if publicURL == "" {
		return "", fmt.Errorf("no public R2 URL configured")
	}

	// The narinfo key is the store hash: strip the compression extension from
	// the NAR basename (e.g. <hash>.nar.xz -> <hash>).
	storeHash := narBasename
	if idx := strings.Index(storeHash, ".nar"); idx != -1 {
		storeHash = storeHash[:idx]
	}

	narinfoURL := fmt.Sprintf("%s/%s.narinfo", strings.TrimRight(publicURL, "/"), storeHash)
	req, err := http.NewRequestWithContext(ctx, "GET", narinfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := ps.HttpShortClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("R2 narinfo HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return fileHashToBlobDigest(string(body))
}

func (ps *ProxyServer) streamBlob(ctx context.Context, w http.ResponseWriter, digest string, contentType string, method string) error {
	token, err := ps.TokenMgr.GetToken(ctx)
	if err != nil {
		return err
	}

	proto := GetProtocol(ps.Registry)
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, digest)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
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
func (ps *ProxyServer) proxyUpstream(w http.ResponseWriter, r *http.Request, upstreamPath string) bool {
	if len(ps.UpstreamCaches) == 0 {
		return false
	}

	for _, cache := range ps.UpstreamCaches {
		upstreamURL := fmt.Sprintf("%s%s", strings.TrimSuffix(cache, "/"), upstreamPath)

		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, nil)
		if err != nil {
			continue // Try next upstream
		}
		req.Header.Set("User-Agent", "aeroflare/1.0")

		resp, err := ps.HttpClient.Do(req)
		if err != nil {
			continue // Try next upstream on network error
		}

		// If it's a 404, we might want to try the next cache.
		// If it's a 200 OK, serve it and return.
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}

		defer func() { _ = resp.Body.Close() }()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for upstream path %s: %v\n", upstreamPath, err)
		}
		return true
	}

	return false
}
