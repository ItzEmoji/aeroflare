package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent            = "aeroflare/1.0"
	ociManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
)

// BootstrapConfig fetches configuration dynamically from the OCI-Registry.
// Now accepts an *http.Client to utilize connection pooling from the caller.
func BootstrapConfig(ctx context.Context, client *http.Client, registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, error) {
	config, _, err := BootstrapConfigWithAnnotations(ctx, client, registry, repository, tokenMgr)
	return config, err
}

func BootstrapConfigWithAnnotations(ctx context.Context, client *http.Client, registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, map[string]string, error) {
	token, err := tokenMgr.GetToken(context.Background())
	if err != nil {
		return nil, nil, err
	}

	// Fallback to avoid panics if nil is passed
	if client == nil {
		client = http.DefaultClient
	}

	proto := GetProtocol(registry)

	// --- 1. Manifest Fetch ---
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/cache-config", proto, registry, repository)
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating manifest request: %w", err)
	}
	req.Header.Set("Accept", ociManifestMediaType)
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	manifestResp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed fetching manifest from %s: %w", manifestURL, err)
	}
	defer func() { _ = manifestResp.Body.Close() }()

	if manifestResp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("config manifest HTTP %d for %s", manifestResp.StatusCode, manifestURL)
	}

	var manifest IndexManifest
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		return nil, nil, fmt.Errorf("failed decoding manifest JSON: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, manifest.Annotations, fmt.Errorf("no layers in cache-config manifest")
	}

	// --- 2. Blob Fetch ---
	digest := manifest.Layers[0].Digest
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", proto, registry, repository, digest)
	blobReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return nil, manifest.Annotations, fmt.Errorf("failed creating blob request: %w", err)
	}
	blobReq.Header.Set("User-Agent", userAgent)
	if token != "" {
		blobReq.Header.Set("Authorization", "Bearer "+token)
	}

	blobResp, err := client.Do(blobReq)
	if err != nil {
		return nil, manifest.Annotations, fmt.Errorf("failed fetching blob from %s: %w", blobURL, err)
	}
	defer func() { _ = blobResp.Body.Close() }()

	if blobResp.StatusCode != http.StatusOK {
		return nil, manifest.Annotations, fmt.Errorf("config blob HTTP %d for %s", blobResp.StatusCode, blobURL)
	}

	var config RemoteConfig
	if err := json.NewDecoder(blobResp.Body).Decode(&config); err != nil {
		return nil, manifest.Annotations, fmt.Errorf("failed decoding blob JSON: %w", err)
	}

	if pk, ok := manifest.Annotations["aeroflare.public-key"]; ok && pk != "" && config.PublicKey == "" {
		config.PublicKey = pk
	} else if pk, ok := manifest.Annotations["public-key"]; ok && pk != "" && config.PublicKey == "" {
		config.PublicKey = pk
	}

	return &config, manifest.Annotations, nil
}

// StartProxy starts the proxy HTTP server on the configured address.
func StartProxy(ctx context.Context, port int, listenAddr string, registry string, repository string, indexDir string, cacheFileName string, indexTTLSeconds int, upstreams []string, githubToken string) (int, error) {
	// --- VALIDATION CHECK ---
	for _, upstream := range upstreams {
		if !IsValidUpstreamURL(upstream) {
			return 0, fmt.Errorf("fatal: invalid upstream URL configured: %q", upstream)
		}
	}

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

	// --- HTTP TRANSPORT & CLIENT TUNING ---
	var transport *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = dt.Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 100
		transport.IdleConnTimeout = 90 * time.Second
	} else {
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	proxyClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Minute, // Kept high for massive NARs
	}

	// Seed the local cache file and refresh in the background
	_ = cacheIndex.loadLocal()
	go func() {
		// context.WithoutCancel (Go 1.21+) decouples this from server startup context
		bgCtx := context.WithoutCancel(ctx)
		cacheIndex.Get(bgCtx)
	}()

	ps := &ProxyServer{
		Port:           port,
		ListenAddr:     listenAddr,
		Registry:       registry,
		Repository:     repository,
		UpstreamCaches: upstreams,
		TokenMgr:       tokenMgr,
		CacheIndex:     cacheIndex,
		HttpClient:     proxyClient,
		HttpShortClient: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ps.Handler)

	addr := net.JoinHostPort(listenAddr, strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	// --- SERVER TUNING ---
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Replaced strict WriteTimeout with IdleTimeout to prevent Slowloris
		// without killing active multi-gigabyte derivations mid-download.
		IdleTimeout: 120 * time.Second,
	}

	serveErr := make(chan error, 1)

	go func() {
		slog.Info("Starting proxy server",
			"listen_addr", listenAddr,
			"port", actualPort,
			"repository", repository,
			"upstream", strings.Join(upstreams, ", "),
			"index_ttl", ttl.String(),
		)
		serveErr <- server.Serve(listener)
	}()

	go func() {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down proxy server...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case err := <-serveErr:
			if err != nil && err != http.ErrServerClosed {
				slog.Error("Server error", "error", err)
			}
		}
	}()

	return actualPort, nil
}
