package proxy

import (
	"context"
	"fmt"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BootstrapConfig fetches the dynamic RemoteConfig (worker URL, public key,
// upstream caches) from the "cache-config" annotations on the OCI registry.
// The caller-supplied *http.Client is reused so its transport's connection
// pool is shared with the rest of the proxy.
func BootstrapConfig(ctx context.Context, client *http.Client, registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, error) {
	config, _, err := BootstrapConfigWithAnnotations(ctx, client, registry, repository, tokenMgr)
	return config, err
}

// BootstrapConfigWithAnnotations is BootstrapConfig, additionally returning
// the raw annotation map so callers (e.g. the CacheIndex refresh path) can
// read fields beyond the ones RemoteConfig models.
func BootstrapConfigWithAnnotations(ctx context.Context, client *http.Client, registry, repository string, tokenMgr *TokenManager) (*RemoteConfig, map[string]string, error) {
	token, err := tokenMgr.GetToken(ctx)
	if err != nil {
		return nil, nil, err
	}

	anns, err := oci.FetchAeroflareAnnotations(ctx, client, registry, repository, "cache-config", token)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch cache-config annotations: %w", err)
	}

	config := &RemoteConfig{}

	// Read directly from aeroflare annotations (assuming fields were stored there)
	if pk, ok := anns["aeroflare.public-key"]; ok && pk != "" {
		config.PublicKey = pk
	}
	if workerURL, ok := anns["aeroflare.worker-url"]; ok && workerURL != "" {
		config.WorkerURL = workerURL
	}
	if upstream, ok := anns["aeroflare.upstream-caches"]; ok && upstream != "" {
		config.UpstreamCaches = strings.Split(upstream, ",")
	}

	return config, anns, nil
}

// StartProxy starts the proxy HTTP server on the configured address.
//
// overrideToken, when non-empty, is used verbatim as the registry bearer token,
// skipping token exchange; pass "" to always exchange. It is supplied by the
// caller rather than read from the environment so the proxy can be embedded.
// Values that are not usable bearer tokens are ignored (see IsBearerToken).
func StartProxy(ctx context.Context, port int, listenAddr string, registry string, repository string, upstreams []string, githubToken string, overrideToken string) (int, error) {
	// --- VALIDATION CHECK ---
	for _, upstream := range upstreams {
		if !IsValidUpstreamURL(upstream) {
			return 0, fmt.Errorf("fatal: invalid upstream URL configured: %q", upstream)
		}
	}

	tokenMgr := NewTokenManager(registry, repository, githubToken)
	tokenMgr.SetOverrideToken(overrideToken)

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

	ps := &ProxyServer{
		Port:           port,
		ListenAddr:     listenAddr,
		Registry:       registry,
		Repository:     repository,
		UpstreamCaches: upstreams,
		TokenMgr:       tokenMgr,
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
		// IdleTimeout (not a strict WriteTimeout) guards against Slowloris-style
		// idle connections without killing active multi-gigabyte NAR downloads
		// mid-transfer.
		IdleTimeout: 120 * time.Second,
	}

	serveErr := make(chan error, 1)

	go func() {
		slog.Info("Starting proxy server",
			"listen_addr", listenAddr,
			"port", actualPort,
			"repository", repository,
			"upstream", strings.Join(upstreams, ", "),
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
