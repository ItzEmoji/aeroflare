// Command aeroflare-ci is a lightweight, non-interactive CI runner that builds
// Nix flake installables and pushes them to one or more OCI caches.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/itzemoji/aeroflare/internal/ci"
)

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// splitEnvList splits a newline- or comma-separated env value into trimmed,
// non-empty entries.
func splitEnvList(v string) []string {
	if v == "" {
		return nil
	}
	sep := func(r rune) bool { return r == '\n' || r == ',' }
	var out []string
	for _, p := range strings.FieldsFunc(v, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var builds, caches, upstreams stringList
	fs := flag.NewFlagSet("aeroflare-ci", flag.ContinueOnError)
	fs.Var(&builds, "build", "flake installable to build (repeatable)")
	fs.Var(&caches, "cache", "<registry>;<repository> push target (repeatable)")
	configPath := fs.String("config", envOr("AEROFLARE_CI_CONFIG", ".aeroflare-ci.yaml"), "config file path")
	compression := fs.String("compression", os.Getenv("AEROFLARE_CI_COMPRESSION"), "compression: zstd, xz, gzip, none")
	signingKey := fs.String("signing-key", os.Getenv("AEROFLARE_CI_SIGNING_KEY"), "signing key path or env var name")
	fs.Var(&upstreams, "upstream-cache", "upstream cache URL (repeatable), or 'none' to disable filtering")
	workers := fs.Int("workers", 0, "concurrent workers (0 = default 50)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if len(builds) == 0 {
		builds = splitEnvList(os.Getenv("AEROFLARE_CI_BUILDS"))
	}
	if len(caches) == 0 {
		caches = splitEnvList(os.Getenv("AEROFLARE_CI_CACHES"))
	}
	if len(upstreams) == 0 {
		upstreams = splitEnvList(os.Getenv("AEROFLARE_CI_UPSTREAM_CACHE"))
	}

	// An explicit --config must exist; the default path is optional.
	required := *configPath != ".aeroflare-ci.yaml"
	fc, _, err := ci.LoadFile(*configPath, required)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aeroflare-ci: %v\n", err)
		os.Exit(1)
	}

	spec, err := ci.Resolve(fc, ci.Inputs{
		Builds:         builds,
		Caches:         caches,
		Compression:    *compression,
		SigningKey:     *signingKey,
		Workers:        *workers,
		UpstreamCaches: upstreams,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "aeroflare-ci: %v\n", err)
		os.Exit(1)
	}

	if ci.Run(spec, os.Stdout) {
		os.Exit(0)
	}
	os.Exit(1)
}
