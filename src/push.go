package network

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aeroflare/src/prepare/cache"

	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/prepare"
	"aeroflare/src/prepare/signing"
)

// Types and index functions are now in index.go

// RunPush implements the 'push' CLI command.
func RunPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	var (
		storePath   string
		inputFile   string
		compression string
		cacheURL    string
		workers     int
		prepareRefs bool
		signingKey  string
		keepFiles   bool
		forcePush   bool
	)

	fs.StringVar(&storePath, "store-path", "", "Nix store path to prepare and push (e.g. /nix/store/xxx-yyy)")
	fs.StringVar(&inputFile, "input", "", "File containing store paths (one per line, # for comments)")
	fs.StringVar(&compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	fs.StringVar(&cacheURL, "cache-url", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	fs.IntVar(&workers, "workers", 50, "Number of concurrent workers")
	fs.BoolVar(&prepareRefs, "prepare-refs", false, "Also prepare references that are not on the upstream cache")
	fs.StringVar(&signingKey, "signing-key", "", "Path to Nix signing private key file")
	fs.BoolVar(&keepFiles, "keep", false, "Keep generated .nar and .narinfo files after the push")
	fs.BoolVar(&forcePush, "force", false, "Force push files even if they exist in the index or upstream cache")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "aeroflare push - Generate NAR archives and upload them directly to the OCI registry\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  aeroflare push --store-path /nix/store/xxx-yyy [flags]\n")
		fmt.Fprintf(os.Stderr, "  aeroflare push --input paths.txt [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	_ = fs.Parse(args)

	if storePath == "" && inputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --store-path or --input is required")
		fs.Usage()
		os.Exit(1)
	}

	// Fetch registry and token
	registry, repository := GetRegistryAndRepository()
	ociToken := GetToken(registry, repository)
	if ociToken == "" {
		fmt.Fprintln(os.Stderr, "Error: Authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
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
			fmt.Fprintf(os.Stderr, "Error loading key: %v\n", err)
			os.Exit(1)
		}
	}

	// Create a temporary directory if files should not be kept
	outputDir, err := os.MkdirTemp("", "aeroflare-push-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temporary directory: %v\n", err)
		os.Exit(1)
	}

	if !keepFiles {
		// Automatic cleanup at the end
		defer func() { _ = os.RemoveAll(outputDir) }()
	} else {
		fmt.Printf("Generated files will be kept in: %s\n", outputDir)
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

	existingIndex, err := FetchCacheIndex(registry, repository, ociToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch cache index: %v\n", err)
		existingIndex = &PushCacheIndex{Entries: make(map[string]PushCacheEntry)}
	}
	if existingIndex.Entries == nil {
		existingIndex.Entries = make(map[string]PushCacheEntry)
	}

	var targetPaths []string
	if storePath != "" {
		targetPaths = append(targetPaths, storePath)
	} else {
		targetPaths, err = prepare.ParseInputFile(inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing input file: %v\n", err)
			os.Exit(1)
		}
	}

	if len(targetPaths) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No store paths found")
		os.Exit(1)
	}

	var filteredPaths []string
	if forcePush {
		filteredPaths = targetPaths
	} else {
		// Filter out paths that are already in the index
		for _, p := range targetPaths {
			basename := filepath.Base(p)
			parts := strings.SplitN(basename, "-", 2)
			if len(parts) >= 2 && existingIndex.Entries[parts[0]].NarDigest != "" {
				fmt.Printf("Skipping %s (already in cache index)\n", p)
				continue
			}
			filteredPaths = append(filteredPaths, p)
		}

		// Filter out paths that are in the upstream cache
		if len(filteredPaths) > 0 && cacheURL != "" {
			c := cache.New(cacheURL, cache.WithMaxConns(workers))

			var hashes []string
			for _, p := range filteredPaths {
				basename := filepath.Base(p)
				parts := strings.SplitN(basename, "-", 2)
				if len(parts) >= 2 {
					hashes = append(hashes, parts[0])
				}
			}

			existsMap, err := c.ExistsBatch(ctx, hashes, workers)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: upstream cache check failed: %v\n", err)
			} else {
				var trulyFiltered []string
				for _, p := range filteredPaths {
					basename := filepath.Base(p)
					parts := strings.SplitN(basename, "-", 2)
					if len(parts) >= 2 && existsMap[parts[0]] {
						fmt.Printf("Skipping %s (already in upstream cache)\n", p)
						continue
					}
					trulyFiltered = append(trulyFiltered, p)
				}
				filteredPaths = trulyFiltered
			}
		}
	}

	if len(filteredPaths) == 0 {
		fmt.Println("No new paths to push.")
		return
	}

	var receipts []PushReceipt

	chunkSize := 100
	for i := 0; i < len(filteredPaths); i += chunkSize {
		end := i + chunkSize
		if end > len(filteredPaths) {
			end = len(filteredPaths)
		}
		chunk := filteredPaths[i:end]

		fmt.Printf("\n--- Processing chunk %d to %d (of %d) ---\n", i+1, end, len(filteredPaths))

		var results []*prepare.Result

		// 1. Prepare (generates .nar and .narinfo)
		if len(chunk) == 1 {
			fmt.Printf("Preparing %s...\n", chunk[0])
			res, err := prepare.Prepare(ctx, chunk[0], cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error during preparation: %v\n", err)
				os.Exit(1)
			}
			results = append(results, res)
		} else {
			res, err := prepare.PrepareBatch(ctx, chunk, cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error during batch preparation: %v\n", err)
				os.Exit(1)
			}
			results = res
		}

		// 2. Push generated files
		pushedPaths := make(map[string]bool)

		var pushResult func(r *prepare.Result, isRoot bool)
		pushResult = func(r *prepare.Result, isRoot bool) {
			if pushedPaths[r.StorePath] {
				return
			}
			pushedPaths[r.StorePath] = true

			if isRoot {
				fmt.Printf("\nUploading: %s\n", r.StorePath)
			} else {
				fmt.Printf("  Uploading referenced dependency: %s\n", r.StorePath)
			}

			narStat, err := os.Stat(r.NarPath)
			if err != nil {
				if isRoot {
					fmt.Fprintf(os.Stderr, "Failed to stat NAR file (%s): %v\n", r.NarPath, err)
					os.Exit(1)
				}
				return
			}

			// Upload NAR
			narDigest, err := PushBlob(r.NarPath, registry, repository, ociToken)
			if err != nil {
				if isRoot {
					fmt.Fprintf(os.Stderr, "Failed to upload NAR file (%s): %v\n", r.NarPath, err)
					os.Exit(1)
				} else {
					fmt.Fprintf(os.Stderr, "    Failed to upload reference NAR: %v\n", err)
					return
				}
			}
			if isRoot {
				fmt.Printf("  NAR successful: %s\n", narDigest)
			}

			// Upload Narinfo
			infoDigest, err := PushBlob(r.NarinfoPath, registry, repository, ociToken)
			if err != nil {
				if isRoot {
					fmt.Fprintf(os.Stderr, "Failed to upload Narinfo file (%s): %v\n", r.NarinfoPath, err)
					os.Exit(1)
				} else {
					fmt.Fprintf(os.Stderr, "    Failed to upload reference Narinfo: %v\n", err)
					return
				}
			}
			if isRoot {
				fmt.Printf("  Narinfo successful: %s\n", infoDigest)
			}

			receipts = append(receipts, PushReceipt{
				StorePath:   r.StorePath,
				NarinfoPath: r.NarinfoPath,
				NarDigest:   narDigest,
				NarSize:     narStat.Size(),
				IsRoot:      isRoot,
			})

			// If references were prepared, recursively push them as well
			for _, missingRef := range r.MissingRefResults {
				pushResult(missingRef, false)
			}
		}

		for _, r := range results {
			pushResult(r, true)
		}
	}

	if err := UpdateCacheIndex(receipts, existingIndex, registry, repository, ociToken, signingKey); err != nil {
		fmt.Fprintf(os.Stderr, "\nFailed to update cache index: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nPush completed successfully.")
}
