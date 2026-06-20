package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/prepare"
	"aeroflare/src/prepare/signing"
	network "aeroflare/src"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <push-blob|pull-blob|proxy|prepare> [args...]\n", os.Args[0])
		os.Exit(1)
	}
	cmd := os.Args[1]

	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		githubToken = os.Getenv("GH_TOKEN")
	}

	switch cmd {
	case "proxy":
		registry, repository := getRegistryAndRepository()

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

		workerURL := os.Getenv("AEROFLARE_WORKER_URL")
		if workerURL == "" {
			workerURL = os.Getenv("NIXCACHE_WORKER_URL")
		}

		err := network.StartProxy(port, listenAddr, registry, repository, indexDir, indexTTL, upstreams, githubToken, workerURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Proxy server failed: %v\n", err)
			os.Exit(1)
		}

	case "push-blob":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s push-blob <file-path>\n", os.Args[0])
			os.Exit(1)
		}
		registry, repository := getRegistryAndRepository()

		ociToken := getToken(registry, repository)
		if ociToken == "" {
			fmt.Fprintln(os.Stderr, "Error: oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		filePath := os.Args[2]
		digest, err := network.PushBlob(filePath, registry, repository, ociToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to push blob: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(digest)

	case "pull-blob":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s pull-blob <digest> <output-file>\n", os.Args[0])
			os.Exit(1)
		}
		registry, repository := getRegistryAndRepository()

		ociToken := getToken(registry, repository)
		if ociToken == "" {
			fmt.Fprintln(os.Stderr, "Error: oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		digest := os.Args[2]
		outFile := os.Args[3]
		err := network.PullBlob(digest, outFile, registry, repository, ociToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to pull blob: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Successfully pulled blob to", outFile)

	case "prepare":
		fs := flag.NewFlagSet("prepare", flag.ExitOnError)
		var (
			storePath   string
			inputFile   string
			outputDir   string
			compression string
			cacheURL    string
			workers     int
			prepareRefs bool
			signingKey  string
		)

		fs.StringVar(&storePath, "store-path", "", "Nix store path to prepare (e.g. /nix/store/xxx-yyy)")
		fs.StringVar(&inputFile, "input", "", "File containing store paths (one per line, # for comments)")
		fs.StringVar(&outputDir, "output-dir", "./output", "Output directory for .nar and .narinfo files")
		fs.StringVar(&compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
		fs.StringVar(&cacheURL, "cache-url", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
		fs.IntVar(&workers, "workers", 50, "Number of concurrent workers")
		fs.BoolVar(&prepareRefs, "prepare-refs", false, "Also prepare NAR+narinfo for references not on the upstream cache (one level deep)")
		fs.StringVar(&signingKey, "signing-key", "", "Path to Nix signing private key file (format: name:base64seed, as produced by 'nix key-gen-secret')")

		fs.Usage = func() {
			fmt.Fprintf(os.Stderr, "aeroflare prepare - Generate NAR archives and narinfo files from Nix store paths\n\n")
			fmt.Fprintf(os.Stderr, "Usage:\n")
			fmt.Fprintf(os.Stderr, "  aeroflare prepare --store-path /nix/store/xxx-yyy [flags]\n")
			fmt.Fprintf(os.Stderr, "  aeroflare prepare --input paths.txt [flags]\n\n")
			fmt.Fprintf(os.Stderr, "Flags:\n")
			fs.PrintDefaults()
		}

		fs.Parse(os.Args[2:])

		if storePath == "" && inputFile == "" {
			fmt.Fprintln(os.Stderr, "Error: --store-path or --input is required")
			fs.Usage()
			os.Exit(1)
		}

		compType, err := compress.ParseType(compression)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		var signKey *signing.PrivateKey
		if signingKey != "" {
			signKey, err = signing.LoadPrivateKey(signingKey)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading signing key: %v\n", err)
				os.Exit(1)
			}
		}

		cfg := &prepare.Config{
			OutputDir:          outputDir,
			Compression:        compType,
			CacheURL:           cacheURL,
			Workers:            workers,
			PrepareMissingRefs: prepareRefs,
			SigningKey:         signKey,
		}

		ctx := context.Background()

		if storePath != "" {
			result, err := prepare.Prepare(ctx, storePath, cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			printResult(result)
		} else {
			paths, err := prepare.ParseInputFile(inputFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if len(paths) == 0 {
				fmt.Fprintln(os.Stderr, "Error: no store paths found in input file")
				os.Exit(1)
			}

			results, err := prepare.PrepareBatch(ctx, paths, cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			totalMissing := 0
			totalPreparedRefs := 0
			totalSigned := 0
			for _, result := range results {
				printResult(result)
				totalMissing += len(result.MissingRefs)
				totalPreparedRefs += len(result.MissingRefResults)
				if result.Signed {
					totalSigned++
				}
			}
			fmt.Printf("\nProcessed %d paths, %d missing references, %d refs prepared, %d signed\n", len(results), totalMissing, totalPreparedRefs, totalSigned)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// getToken attempts to get a valid token, exchanging a GitHub PAT if necessary
func getToken(registry, repository string) string {
	if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t // Token seems to be a valid Bearer token already
	}

	cred := os.Getenv("GITHUB_TOKEN")
	if cred == "" {
		cred = os.Getenv("GH_TOKEN")
	}
	if cred == "" {
		return os.Getenv("oci_token")
	}

	// Try to exchange it
	exchanged, err := network.ExchangeToken(registry, repository, cred)
	if err == nil && exchanged != "" {
		return exchanged
	}

	return cred // Fallback
}

// getRegistryAndRepository computes the registry and repository from environment variables.
func getRegistryAndRepository() (string, string) {
	registry := os.Getenv("AEROFLARE_REGISTRY")
	if registry == "" {
		registry = os.Getenv("NIXCACHE_REGISTRY")
	}
	if registry == "" {
		registry = "ghcr.io"
	}

	ociURL := os.Getenv("AEROFLARE_OCI_URL")
	var repository string

	if ociURL != "" {
		ociURL = strings.TrimPrefix(ociURL, "oci://")
		parts := strings.SplitN(ociURL, "/", 2)
		if len(parts) == 2 && strings.Contains(parts[0], ".") {
			registry = parts[0]
			repository = parts[1]
		} else {
			repository = ociURL
		}
	} else {
		cacheName := os.Getenv("AEROFLARE_CACHE")
		if cacheName == "" {
			cacheName = os.Getenv("NIXCACHE_REPO")
		}
		if cacheName == "" {
			fmt.Fprintln(os.Stderr, "Error: AEROFLARE_CACHE or AEROFLARE_OCI_URL environment variable is required")
			os.Exit(1)
		}
		cacheName = strings.ToLower(cacheName)
		repository = fmt.Sprintf("%s/nix-cache", cacheName)
	}

	return registry, repository
}

// printResult prints the result of a prepare operation
func printResult(r *prepare.Result) {
	fmt.Printf("Prepared: %s\n", r.StorePath)
	fmt.Printf("  NAR:     %s\n", r.NarPath)
	fmt.Printf("  Narinfo: %s\n", r.NarinfoPath)
	if r.Signed {
		fmt.Printf("  Signed:  yes\n")
	}
	if len(r.MissingRefs) > 0 {
		fmt.Printf("  Missing references (%d, not on upstream cache):\n", len(r.MissingRefs))
		for _, ref := range r.MissingRefs {
			fmt.Printf("    %s\n", filepath.Base(ref))
		}
		if len(r.MissingRefResults) > 0 {
			fmt.Printf("  Prepared missing refs (%d):\n", len(r.MissingRefResults))
			for _, rr := range r.MissingRefResults {
				fmt.Printf("    %s -> %s, %s\n", filepath.Base(rr.StorePath), filepath.Base(rr.NarPath), filepath.Base(rr.NarinfoPath))
			}
		}
	} else if len(r.References) > 0 {
		fmt.Printf("  All %d references found on upstream cache\n", len(r.References))
	}
}
