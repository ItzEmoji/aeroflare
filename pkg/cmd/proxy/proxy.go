// Package proxy implements `aeroflare proxy`, which starts the cache proxy
// server.
package proxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	proxysrv "github.com/itzemoji/aeroflare/pkg/proxy"

	"github.com/spf13/cobra"
)

// Options holds the dependencies proxyRun needs.
type Options struct {
	IO *iostreams.IOStreams

	// Token is an explicit registry token from the --token flag. When set it
	// takes precedence over every other source (see resolveProxyToken).
	Token string
}

// NewCmdProxy builds the `aeroflare proxy` command.
func NewCmdProxy(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Start the cache proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return proxyRun(f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Token, "token", "", "Registry token to authenticate with (overrides NIXCACHE_TOKEN and the saved credential)")

	return cmd
}

// resolveProxyToken returns the registry token the proxy authenticates with,
// checking in priority order: the --token flag, the NIXCACHE_TOKEN environment
// variable (the same NIXCACHE_* convention proxySettingsFromEnv uses, so the
// proxy is configured identically whether run directly, as a service, or in a
// container), then the saved registry credential via OptionalTokenForRegistry.
//
// Unlike the Worker's base64-encoded NIXCACHE_TOKEN secret, the value here is a
// raw registry token/PAT: it becomes the Basic-auth password / token-exchange
// credential against the registry.
func resolveProxyToken(f *cmdutil.Factory, opts *Options, registry string) string {
	if opts.Token != "" {
		return opts.Token
	}
	if t := os.Getenv("NIXCACHE_TOKEN"); t != "" {
		return t
	}
	return shared.OptionalTokenForRegistry(f, registry)
}

// proxySettingsFromEnv reads the proxy's listen settings from NIXCACHE_* env
// vars rather than flags, so the proxy can be configured the same way whether
// it's run directly or deployed as a systemd service / container. An
// unparseable NIXCACHE_PORT falls back to the default rather than failing.
func proxySettingsFromEnv() (port int, listenAddr string, upstreams []string) {
	port = 8080
	if pStr := os.Getenv("NIXCACHE_PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			port = p
		}
	}

	listenAddr = os.Getenv("NIXCACHE_LISTEN")
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}

	if ups := os.Getenv("NIXCACHE_UPSTREAM"); ups != "" {
		upstreams = strings.Fields(ups)
	} else {
		upstreams = []string{"https://cache.nixos.org"}
	}

	return port, listenAddr, upstreams
}

// proxyDisplayHost maps the bind address to a host usable in a clickable URL.
// A wildcard bind (0.0.0.0 / ::) or an empty address is not reachable as-is, so
// the printed link points at loopback, which does reach the local listener.
func proxyDisplayHost(listenAddr string) string {
	switch listenAddr {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	}
	return listenAddr
}

func proxyRun(f *cmdutil.Factory, opts *Options) error {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}

	port, listenAddr, upstreams := proxySettingsFromEnv()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel the proxy's context on SIGINT/SIGTERM so StartProxy can shut
	// down its listener cleanly instead of the process being killed mid-request.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	actualPort, err := proxysrv.StartProxy(ctx, port, listenAddr, registry, repository, upstreams, cmdutil.RegistryAuth(registry, resolveProxyToken(f, opts, registry)))
	if err != nil {
		return fmt.Errorf("proxy server failed: %w", err)
	}
	// Print a clean http:// URL so terminals render it as a clickable link
	// straight to the proxy (no trailing punctuation, which some terminals
	// would swallow into the link). JoinHostPort brackets IPv6 hosts so the
	// URL stays valid (e.g. http://[::1]:8080).
	hostPort := net.JoinHostPort(proxyDisplayHost(listenAddr), strconv.Itoa(actualPort))
	opts.IO.Info(fmt.Sprintf("Started proxy on http://%s", hostPort))

	<-ctx.Done()

	return nil
}
