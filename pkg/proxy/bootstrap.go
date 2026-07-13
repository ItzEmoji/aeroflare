package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/itzemoji/aeroflare/pkg/oci"
)

// BootstrapConfig fetches the dynamic RemoteConfig (worker URL, public key,
// upstream caches) from the "cache-config" annotations on the OCI registry.
// A nil auth reads anonymously.
func BootstrapConfig(ctx context.Context, registry, repository string, auth authn.Authenticator) (*RemoteConfig, error) {
	config, _, err := BootstrapConfigWithAnnotations(ctx, registry, repository, auth)
	return config, err
}

// BootstrapConfigWithAnnotations is BootstrapConfig, additionally returning
// the raw annotation map so callers can read fields beyond the ones
// RemoteConfig models.
func BootstrapConfigWithAnnotations(ctx context.Context, registry, repository string, auth authn.Authenticator) (*RemoteConfig, map[string]string, error) {
	anns, err := oci.FetchAeroflareAnnotations(ctx, registry, repository, "cache-config", auth)
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
// auth is the registry credential; a nil value reads anonymously, which is all
// a public cache needs. It is supplied by the caller rather than read from the
// environment so the proxy can be embedded. The credential is negotiated with
// the registry on first use and refreshed when it expires, so a long-running
// proxy needs no token upkeep.
func StartProxy(ctx context.Context, port int, listenAddr string, registry string, repository string, upstreams []string, auth authn.Authenticator) (int, error) {
	// --- VALIDATION CHECK ---
	for _, upstream := range upstreams {
		if !IsValidUpstreamURL(upstream) {
			return 0, fmt.Errorf("fatal: invalid upstream URL configured: %q", upstream)
		}
	}

	ps, err := NewProxyServer(registry, repository, upstreams, auth)
	if err != nil {
		return 0, err
	}
	ps.Port = port
	ps.ListenAddr = listenAddr

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
