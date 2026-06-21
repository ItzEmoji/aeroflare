package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	network "aeroflare/src"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the cache proxy server",
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		port := 37515
		if pStr := os.Getenv("NIXCACHE_PORT"); pStr != "" {
			if p, err := strconv.Atoi(pStr); err == nil {
				port = p
			}
		}

		listenAddr := os.Getenv("NIXCACHE_LISTEN")
		if listenAddr == "" {
			listenAddr = "127.0.0.1"
		}

		indexTTL := 300
		if ttlStr := os.Getenv("NIXCACHE_INDEX_TTL"); ttlStr != "" {
			if t, err := strconv.Atoi(ttlStr); err == nil {
				indexTTL = t
			}
		}

		var upstreams []string
		if ups := os.Getenv("NIXCACHE_UPSTREAM"); ups != "" {
			upstreams = strings.Fields(ups)
		} else {
			upstreams = []string{"https://cache.nixos.org"}
		}

		indexDir := getIndexDir(repository)

		workerURL := os.Getenv("AEROFLARE_WORKER_URL")
		if workerURL == "" {
			workerURL = os.Getenv("NIXCACHE_WORKER_URL")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			cancel()
		}()

		actualPort, err := network.StartProxy(ctx, port, listenAddr, registry, repository, indexDir, indexTTL, upstreams, getGithubToken(), workerURL)
		if err != nil {
			PrintError(fmt.Sprintf("Proxy server failed: %v", err))
			os.Exit(1)
		}
		PrintInfo(fmt.Sprintf("Started proxy on %s:%d...", listenAddr, actualPort))

		<-ctx.Done()
	},
}

func init() {
	rootCmd.AddCommand(proxyCmd)
}

func getIndexDir(repository string) string {
	indexDir := os.Getenv("AEROFLARE_INDEX_DIR")
	if indexDir == "" {
		indexDir = os.Getenv("NIXCACHE_INDEX_DIR")
	}
	if indexDir == "" {
		if cacheDir := os.Getenv("CACHE_DIRECTORY"); cacheDir != "" {
			indexDir = cacheDir
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = os.TempDir()
			}
			repoSlug := strings.ReplaceAll(repository, "/", "--")
			indexDir = filepath.Join(home, ".cache", "aeroflare-proxy", repoSlug)
		}
	}
	return indexDir
}
