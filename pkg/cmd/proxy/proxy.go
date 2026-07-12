// Package proxy implements `aeroflare proxy`, which starts the cache proxy
// server.
package proxy

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	proxysrv "github.com/itzemoji/aeroflare/pkg/proxy"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/spf13/cobra"
)

// Options holds the dependencies proxyRun needs.
type Options struct {
	IO *iostreams.IOStreams
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

	return cmd
}

// proxySettingsFromEnv reads the proxy's listen settings from NIXCACHE_* env
// vars rather than flags, so the proxy can be configured the same way whether
// it's run directly or deployed as a systemd service / container. An
// unparseable NIXCACHE_PORT falls back to the default rather than failing.
func proxySettingsFromEnv() (port int, listenAddr string, upstreams []string) {
	port = 37515
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

	token := shared.OptionalTokenForRegistry(f, registry)
	actualPort, err := proxysrv.StartProxy(ctx, port, listenAddr, registry, repository, upstreams, token, cmdutil.RegistryOverrideToken())
	if err != nil {
		return fmt.Errorf("proxy server failed: %w", err)
	}
	opts.IO.Info(fmt.Sprintf("Started proxy on %s:%d...", listenAddr, actualPort))

	<-ctx.Done()

	return nil
}
