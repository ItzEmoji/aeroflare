package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	network "aeroflare/src"
	"aeroflare/src/prepare/cache"
	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/prepare"
	"aeroflare/src/prepare/signing"

	"github.com/spf13/cobra"
)

var (
	pushStorePath   string
	pushInputFile   string
	pushCompression string
	pushCacheURL    string
	pushWorkers     int
	pushPrepareRefs bool
	pushSigningKey  string
	pushKeepFiles   bool
	pushForcePush   bool
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push a build to the cache",
	Run: func(cmd *cobra.Command, args []string) {
		startTime := time.Now()
		var totalUploaded int



		// Fetch registry and token
		registry, repository := network.GetRegistryAndRepository()
		ociToken := network.GetToken(registry, repository)
		if ociToken == "" {
			PrintError("Authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
			os.Exit(1)
		}

		compType, err := compress.ParseType(pushCompression)
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		var signKey *signing.PrivateKey
		if pushSigningKey != "" {
			signKey, err = signing.LoadPrivateKey(pushSigningKey)
			if err != nil {
				PrintError(fmt.Sprintf("Error loading key: %v", err))
				os.Exit(1)
			}
		}

		// Create a temporary directory if files should not be kept
		outputDir, err := os.MkdirTemp("", "aeroflare-push-*")
		if err != nil {
			PrintError(fmt.Sprintf("Error creating temporary directory: %v", err))
			os.Exit(1)
		}

		if !pushKeepFiles {
			defer func() { _ = os.RemoveAll(outputDir) }()
		} else {
			fmt.Printf("Generated files will be kept in: %s\n", outputDir)
		}

		cfg := &prepare.Config{
			OutputDir:          outputDir,
			Compression:        compType,
			CacheURL:           pushCacheURL,
			Workers:            pushWorkers,
			PrepareMissingRefs: pushPrepareRefs,
			SigningKey:         signKey,
		}

		ctx := context.Background()

		existingIndex, err := network.FetchCacheIndex(registry, repository, ociToken)
		if err != nil {
			PrintWarning(fmt.Sprintf("failed to fetch cache index: %v", err))
			existingIndex = &network.PushCacheIndex{Entries: make(map[string]network.PushCacheEntry)}
		}
		if existingIndex.Entries == nil {
			existingIndex.Entries = make(map[string]network.PushCacheEntry)
		}

		var targetPaths []string
		if pushStorePath != "" {
			targetPaths = append(targetPaths, pushStorePath)
		}
		if pushInputFile != "" {
			filePaths, err := prepare.ParseInputFile(pushInputFile)
			if err != nil {
				PrintError(fmt.Sprintf("Error parsing input file: %v", err))
				os.Exit(1)
			}
			targetPaths = append(targetPaths, filePaths...)
		}
		
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" && !strings.HasPrefix(line, "#") {
					targetPaths = append(targetPaths, line)
				}
			}
		}

		if len(targetPaths) == 0 {
			PrintError("No store paths found. Provide --store-path, --input, or pipe paths via stdin.")
			_ = cmd.Usage()
			os.Exit(1)
		}

		var filteredPaths []string
		if pushForcePush {
			filteredPaths = targetPaths
		} else {
			for _, p := range targetPaths {
				basename := filepath.Base(p)
				parts := strings.SplitN(basename, "-", 2)
				if len(parts) >= 2 && existingIndex.Entries[parts[0]].NarDigest != "" {
					fmt.Printf("Skipping %s (already in cache index)\n", p)
					continue
				}
				filteredPaths = append(filteredPaths, p)
			}

			if len(filteredPaths) > 0 && pushCacheURL != "" {
				c := cache.New(pushCacheURL, cache.WithMaxConns(pushWorkers))

				var hashes []string
				for _, p := range filteredPaths {
					basename := filepath.Base(p)
					parts := strings.SplitN(basename, "-", 2)
					if len(parts) >= 2 {
						hashes = append(hashes, parts[0])
					}
				}

				existsMap, err := c.ExistsBatch(ctx, hashes, pushWorkers)
				if err != nil {
					PrintWarning(fmt.Sprintf("upstream cache check failed: %v", err))
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

		var receipts []network.PushReceipt

		chunkSize := 100
		for i := 0; i < len(filteredPaths); i += chunkSize {
			end := i + chunkSize
			if end > len(filteredPaths) {
				end = len(filteredPaths)
			}
			chunk := filteredPaths[i:end]

			numChunks := (len(filteredPaths) + chunkSize - 1) / chunkSize
			currentChunk := (i / chunkSize) + 1
			fmt.Printf("\n--- Processing chunk %d/%d ---\n\n", currentChunk, numChunks)

			var results []*prepare.Result

			fmt.Println("Step 1/2: Preparing (Generating NAR and narinfo files)")
			if len(chunk) == 1 {
				fmt.Printf("Path: %s\n\n", chunk[0])
				res, err := prepare.Prepare(ctx, chunk[0], cfg)
				if err != nil {
					PrintError(fmt.Sprintf("Error during preparation: %v", err))
					os.Exit(1)
				}
				results = append(results, res)
			} else {
				fmt.Printf("Paths: %d\n\n", len(chunk))
				res, err := prepare.PrepareBatch(ctx, chunk, cfg)
				if err != nil {
					PrintError(fmt.Sprintf("Error during batch preparation: %v", err))
					os.Exit(1)
				}
				results = res
			}

			fmt.Println("Step 2/2: Uploading to OCI registry")
			pushedPaths := make(map[string]bool)

			var pushResult func(r *prepare.Result, isRoot bool)
			pushResult = func(r *prepare.Result, isRoot bool) {
				if pushedPaths[r.StorePath] {
					return
				}
				pushedPaths[r.StorePath] = true

				narStat, err := os.Stat(r.NarPath)
				if err != nil {
					if isRoot {
						PrintError(fmt.Sprintf("Failed to stat NAR file (%s): %v", r.NarPath, err))
						os.Exit(1)
					}
					return
				}

				narDigest, err := network.PushBlob(r.NarPath, registry, repository, ociToken)
				if err != nil {
					if isRoot {
						PrintError(fmt.Sprintf("Failed to upload NAR file (%s): %v", r.NarPath, err))
						os.Exit(1)
					} else {
						PrintError(fmt.Sprintf("    Failed to upload reference NAR: %v", err))
						return
					}
				}

				_, err = network.PushBlob(r.NarinfoPath, registry, repository, ociToken)
				if err != nil {
					if isRoot {
						PrintError(fmt.Sprintf("Failed to upload Narinfo file (%s): %v", r.NarinfoPath, err))
						os.Exit(1)
					} else {
						PrintError(fmt.Sprintf("    Failed to upload reference Narinfo: %v", err))
						return
					}
				}

				pkgName := filepath.Base(r.StorePath)
				fmt.Println("✔ " + pkgName)
				totalUploaded++

				receipts = append(receipts, network.PushReceipt{
					StorePath:   r.StorePath,
					NarinfoPath: r.NarinfoPath,
					NarDigest:   narDigest,
					NarSize:     narStat.Size(),
					IsRoot:      isRoot,
				})

				for _, missingRef := range r.MissingRefResults {
					pushResult(missingRef, false)
				}
			}

			for _, r := range results {
				pushResult(r, true)
			}
		}

		fmt.Println("\nUpdating cache index...")
		if err := network.UpdateCacheIndex(receipts, existingIndex, registry, repository, ociToken, pushSigningKey); err != nil {
			PrintError(fmt.Sprintf("Failed to update cache index: %v", err))
			os.Exit(1)
		}
		fmt.Println("✔ Cache index updated")

		duration := time.Since(startTime)
		
		rootsUploaded := 0
		for _, r := range receipts {
			if r.IsRoot {
				rootsUploaded++
			}
		}
		fmt.Println("\nSummary")
		fmt.Println("────────────────────────────────")
		fmt.Println()
		fmt.Printf("Packages uploaded: %d\n", totalUploaded)
		fmt.Printf("GC roots:          %d\n", rootsUploaded)
		fmt.Printf("Duration:          %s\n\n", duration.Round(time.Millisecond))
		
		fmt.Println("Done.")
	},
}

func init() {
	pushCmd.Flags().StringVar(&pushStorePath, "store-path", "", "Nix store path to prepare and push (e.g. /nix/store/xxx-yyy)")
	pushCmd.Flags().StringVar(&pushInputFile, "input", "", "File containing store paths (one per line, # for comments)")
	pushCmd.Flags().StringVar(&pushCompression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	pushCmd.Flags().StringVar(&pushCacheURL, "cache-url", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	pushCmd.Flags().IntVar(&pushWorkers, "workers", 50, "Number of concurrent workers")
	pushCmd.Flags().BoolVar(&pushPrepareRefs, "prepare-refs", true, "Also prepare references that are not on the upstream cache")
	pushCmd.Flags().StringVar(&pushSigningKey, "signing-key", "", "Path to Nix signing private key file")
	pushCmd.Flags().BoolVar(&pushKeepFiles, "keep", false, "Keep generated .nar and .narinfo files after the push")
	pushCmd.Flags().BoolVar(&pushForcePush, "force", false, "Force push files even if they exist in the index or upstream cache")

	rootCmd.AddCommand(pushCmd)
}
