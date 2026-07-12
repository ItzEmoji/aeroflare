package proxy

import (
	"github.com/itzemoji/aeroflare/pkg/oci"
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
//
// It holds no cache of its own: every narinfo, NAR, and public-key lookup is
// resolved with a direct request to the OCI registry (the per-package manifest
// tagged with the store hash, or the "cache-config" manifest for the public
// key). There is no index blob, no TTL, and nothing to refresh.
type ProxyServer struct {
	Port            int
	ListenAddr      string
	Registry        string
	Repository      string
	UpstreamCaches  []string
	TokenMgr        *TokenManager
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
			ps.serveApiPublicKey(w, r)
		case path == "/_status":
			ps.serveStatus(w, r)
		case strings.HasSuffix(path, ".narinfo"):
			ps.serveNarInfo(w, r, path)
		case strings.HasPrefix(path, "/nar/"):
			ps.serveNar(w, r, path, r.Method)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// fetchPublicKey fetches the cache's public key straight from the
// "cache-config" OCI manifest annotations. There is no caching: each call is a
// fresh registry request. Returns "" if the manifest can't be fetched or has no
// key configured.
func (ps *ProxyServer) fetchPublicKey(ctx context.Context) string {
	token, err := ps.TokenMgr.GetToken(ctx)
	if err != nil {
		return ""
	}
	anns, err := oci.FetchAeroflareAnnotations(ctx, ps.HttpShortClient, ps.Registry, ps.Repository, "cache-config", token)
	if err != nil {
		return ""
	}
	if pk := anns["aeroflare.public-key"]; pk != "" {
		return pk
	}
	return anns["public-key"]
}

func (ps *ProxyServer) serveNixCacheInfo(w http.ResponseWriter) {
	data := []byte("StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n")
	w.Header().Set("Content-Type", "text/x-nix-cache-info")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (ps *ProxyServer) servePublicKey(w http.ResponseWriter, r *http.Request) {
	pubKey := ps.fetchPublicKey(r.Context())

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

// serveApiPublicKey is an alias of servePublicKey kept for API compatibility.
func (ps *ProxyServer) serveApiPublicKey(w http.ResponseWriter, r *http.Request) {
	ps.servePublicKey(w, r)
}

func (ps *ProxyServer) serveStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"repo":     ps.Repository,
		"upstream": ps.UpstreamCaches,
		"mode":     "native",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// serveNarInfo resolves a "<hash>.narinfo" request by reconstructing it from
// the native OCI manifest tagged with the store hash, falling back to the
// upstream caches if the manifest can't satisfy the request.
func (ps *ProxyServer) serveNarInfo(w http.ResponseWriter, r *http.Request, path string) {
	storeHash := strings.TrimPrefix(path, "/")
	storeHash = strings.TrimSuffix(storeHash, ".narinfo")

	if err := ps.serveNativeNarinfo(w, r, storeHash); err == nil {
		return
	}

	// Fallback to upstream cache.
	upstreamPath := fmt.Sprintf("/%s.narinfo", storeHash)
	if ps.proxyUpstream(w, r, upstreamPath) {
		return
	}

	http.Error(w, "Narinfo Not Found", http.StatusNotFound)
}

// serveNar resolves a "/nar/<basename>" request by deriving the GHCR blob
// digest from the native OCI manifest tagged with the store hash, streaming it
// from GHCR if found, and otherwise falling back to the upstream caches.
func (ps *ProxyServer) serveNar(w http.ResponseWriter, r *http.Request, path string, method string) {
	narBasename := strings.TrimPrefix(path, "/nar/")
	contentType := "application/x-nix-nar"
	if strings.HasSuffix(narBasename, ".xz") {
		contentType = "application/x-xz"
	}

	var digest string
	if d, err := ps.digestFromNativeManifest(r.Context(), narBasename); err == nil && d != "" {
		digest = d
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to derive digest from native manifest for %s: %v. Trying upstream...\n", narBasename, err)
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

// serveNativeNarinfo reconstructs a narinfo file from the OCI manifest tagged
// with storeHash (native mode): the aeroflare.* fields are read from the
// manifest's annotations/labels, falling back to the image config's labels
// via fetchConfigLabels if the manifest itself doesn't carry them.
func (ps *ProxyServer) serveNativeNarinfo(w http.ResponseWriter, r *http.Request, storeHash string) error {
	proto := oci.GetProtocol(ps.Registry)
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

	if manifest.Annotations != nil && manifest.Annotations["aeroflare.storepath"] != "" {
		labels = manifest.Annotations
	} else if manifest.Labels != nil && manifest.Labels["aeroflare.storepath"] != "" {
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
	fmt.Fprintf(&b, "StorePath: %s\n", labels["aeroflare.storepath"])
	fmt.Fprintf(&b, "URL: %s\n", labels["aeroflare.url"])
	fmt.Fprintf(&b, "Compression: %s\n", labels["aeroflare.compression"])
	fmt.Fprintf(&b, "FileHash: %s\n", labels["aeroflare.filehash"])
	fmt.Fprintf(&b, "FileSize: %s\n", labels["aeroflare.filesize"])
	fmt.Fprintf(&b, "NarHash: %s\n", labels["aeroflare.narhash"])
	fmt.Fprintf(&b, "NarSize: %s\n", labels["aeroflare.narsize"])

	if rStr, ok := labels["aeroflare.references"]; ok && rStr != "" {
		fmt.Fprintf(&b, "References: %s\n", rStr)
	} else {
		// The trailing space is required: Nix reads a field's value at
		// (colon + 2), so a bare "References:\n" makes it consume the following
		// line (e.g. "Deriver: ...drv") as the references value and fail with
		// "'Deriver:' is too short to be a valid store path".
		b.WriteString("References: \n")
	}

	// Only emit Deriver when the annotation actually carries one. Nix parses the
	// value as a store-path basename, so a bare "Deriver:" (empty value) fails
	// with "'Deriver:' is too short to be a valid store path". Omitting the line
	// is how "no known deriver" is represented (e.g. fetched sources).
	if deriver := labels["aeroflare.deriver"]; deriver != "" {
		fmt.Fprintf(&b, "Deriver: %s\n", deriver)
	}

	if system, ok := labels["aeroflare.system"]; ok && system != "" {
		fmt.Fprintf(&b, "System: %s\n", system)
	}

	if sig, ok := labels["aeroflare.sig"]; ok && sig != "" {
		fmt.Fprintf(&b, "Sig: %s\n", sig)
	}

	body := []byte(b.String())
	w.Header().Set("Content-Type", "text/x-nix-narinfo")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return nil
}

// digestFromNativeManifest fetches the OCI manifest tagged with the NAR's
// store hash (native mode) and returns its first layer's digest, which is
// the GHCR blob digest for that NAR.
func (ps *ProxyServer) digestFromNativeManifest(ctx context.Context, narBasename string) (string, error) {
	tag := narBasename
	if idx := strings.Index(tag, ".nar"); idx != -1 {
		tag = tag[:idx]
	}

	proto := oci.GetProtocol(ps.Registry)
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

// fetchConfigLabels downloads the OCI image config blob at configDigest and
// returns its labels, checking both the "Labels"/"labels" key at the top
// level and nested under "config" (different tools populate different
// locations). Returns a nil map (no error) if no labels are present anywhere.
func (ps *ProxyServer) fetchConfigLabels(ctx context.Context, configDigest string) (map[string]string, error) {
	proto := oci.GetProtocol(ps.Registry)
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, configDigest)
	req, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
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

// streamBlob fetches the blob at digest from GHCR and copies it directly to
// w. If the copy is interrupted partway through, it panics with
// http.ErrAbortHandler rather than returning normally, so the client sees a
// broken connection instead of what looks like a complete-but-truncated body.
func (ps *ProxyServer) streamBlob(ctx context.Context, w http.ResponseWriter, digest string, contentType string, method string) error {
	token, err := ps.TokenMgr.GetToken(ctx)
	if err != nil {
		return err
	}

	proto := oci.GetProtocol(ps.Registry)
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, ps.Registry, ps.Repository, digest)
	req, err := http.NewRequestWithContext(ctx, method, blobURL, nil)
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
		// Abort the connection so the client sees a transfer error; returning
		// normally would cleanly terminate a truncated body, making it look
		// like a complete download.
		panic(http.ErrAbortHandler)
	}
	return nil
}

// proxyUpstream tries each configured upstream cache in order for
// upstreamPath, streaming and returning true on the first 200 OK response.
// Non-200 responses and network errors move on to the next upstream; it
// returns false once all upstreams have been tried (or none are configured).
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
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for upstream path %s: %v\n", upstreamPath, err)
			}
			// Abort instead of cleanly terminating a truncated body.
			panic(http.ErrAbortHandler)
		}
		return true
	}

	return false
}
