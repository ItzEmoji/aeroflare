package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// BootstrapConfig fetches configuration dynamically from the GHCR 'cache-config' OCI image/blob.
// Used by the push/configure pipeline; the proxy itself reads from the cache-index manifest.
func BootstrapConfig(registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, error) {
	config, _, err := BootstrapConfigWithAnnotations(registry, repository, tokenMgr)
	return config, err
}

func BootstrapConfigWithAnnotations(registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, map[string]string, error) {
	token, err := tokenMgr.GetToken()
	if err != nil {
		return nil, nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	proto := GetProtocol(registry)

	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-config", proto, registry, repository)
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("config manifest HTTP %d", resp.StatusCode)
	}

	var manifest IndexManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, nil, err
	}

	if len(manifest.Layers) == 0 {
		return nil, manifest.Annotations, fmt.Errorf("no layers in cache-config manifest")
	}

	digest := manifest.Layers[0].Digest
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, digest)
	req, err = http.NewRequest("GET", blobURL, nil)
	if err != nil {
		return nil, manifest.Annotations, err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err = client.Do(req)
	if err != nil {
		return nil, manifest.Annotations, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, manifest.Annotations, fmt.Errorf("config blob HTTP %d", resp.StatusCode)
	}

	var config RemoteConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, manifest.Annotations, err
	}

	if pk, ok := manifest.Annotations["public-key"]; ok && pk != "" && config.PublicKey == "" {
		config.PublicKey = pk
	}

	return &config, manifest.Annotations, nil
}

// StartProxy starts the proxy HTTP server on the configured address.
func StartProxy(ctx context.Context, port int, listenAddr string, registry string, repository string, indexDir string, cacheFileName string, indexTTLSeconds int, upstreams []string, githubToken string) (int, error) {
	tokenMgr := NewTokenManager(registry, repository, githubToken)

	if cacheFileName == "" {
		cacheFileName = "cache-index.json"
	}

	ttl := time.Duration(indexTTLSeconds) * time.Second
	cacheIndex := &CacheIndex{
		IndexDir:      indexDir,
		CacheFileName: cacheFileName,
		IndexTTL:      ttl,
		TokenMgr:      tokenMgr,
		Registry:      registry,
		Repository:    repository,
	}

	// Seed the local cache file (if present) so the very first request doesn't
	// block on a registry round-trip, then refresh in the background.
	_ = cacheIndex.loadLocal()
	go func() {
		_, _ = cacheIndex.Get()
	}()

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
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Starting proxy server on http://%s:%d\n", listenAddr, actualPort)
		fmt.Fprintf(os.Stderr, "  Repo: %s\n", repository)
		fmt.Fprintf(os.Stderr, "  Upstream: %s\n", strings.Join(upstreams, ", "))
		fmt.Fprintf(os.Stderr, "  Index TTL: %s\n", ttl)

		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	go func() {
		<-ctx.Done()
		fmt.Fprintf(os.Stderr, "\n[aeroflare proxy] Shutting down...\n")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return actualPort, nil
}
