package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/prepare"
	"aeroflare/src/prepare/signing"

	"github.com/spf13/cobra"
)

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

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Generate NAR archives and narinfo files from Nix store paths",
	Run: func(cmd *cobra.Command, args []string) {
		if storePath == "" && inputFile == "" {
			PrintError("--store-path or --input is required")
			_ = cmd.Usage()
			os.Exit(1)
		}

		compType, err := compress.ParseType(compression)
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		var signKey *signing.PrivateKey
		if signingKey != "" {
			signKey, err = signing.LoadPrivateKey(signingKey)
			if err != nil {
				PrintError(fmt.Sprintf("Error loading signing key: %v", err))
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
			PrintInfo(fmt.Sprintf("Preparing store path: %s", storePath))
			result, err := prepare.Prepare(ctx, storePath, cfg)
			if err != nil {
				PrintError(err.Error())
				os.Exit(1)
			}
			printResult(result)
		} else {
			paths, err := prepare.ParseInputFile(inputFile)
			if err != nil {
				PrintError(err.Error())
				os.Exit(1)
			}
			if len(paths) == 0 {
				PrintError("no store paths found in input file")
				os.Exit(1)
			}

			PrintInfo(fmt.Sprintf("Preparing %d paths from input file...", len(paths)))
			results, err := prepare.PrepareBatch(ctx, paths, cfg)
			if err != nil {
				PrintError(err.Error())
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
			
			summary := fmt.Sprintf("Processed %d paths, %d missing references, %d refs prepared, %d signed", len(results), totalMissing, totalPreparedRefs, totalSigned)
			PrintSuccess(summary)
		}
	},
}

func init() {
	prepareCmd.Flags().StringVar(&storePath, "store-path", "", "Nix store path to prepare (e.g. /nix/store/xxx-yyy)")
	prepareCmd.Flags().StringVar(&inputFile, "input", "", "File containing store paths (one per line, # for comments)")
	prepareCmd.Flags().StringVar(&outputDir, "output-dir", "./output", "Output directory for .nar and .narinfo files")
	prepareCmd.Flags().StringVar(&compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	prepareCmd.Flags().StringVar(&cacheURL, "cache-url", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	prepareCmd.Flags().IntVar(&workers, "workers", 50, "Number of concurrent workers")
	prepareCmd.Flags().BoolVar(&prepareRefs, "prepare-refs", false, "Also prepare NAR+narinfo for references not on the upstream cache (one level deep)")
	prepareCmd.Flags().StringVar(&signingKey, "signing-key", "", "Path to Nix signing private key file (format: name:base64seed, as produced by 'nix key-gen-secret')")
	
	rootCmd.AddCommand(prepareCmd)
}

func printResult(r *prepare.Result) {
	fmt.Println("Prepared: " + r.StorePath)
	fmt.Println("  NAR:     " + r.NarPath)
	fmt.Println("  Narinfo: " + r.NarinfoPath)
	if r.Signed {
		fmt.Println("  Signed:  yes")
	}
	if len(r.MissingRefs) > 0 {
		fmt.Printf("  Missing references (%d, not on upstream cache):\n", len(r.MissingRefs))
		for _, ref := range r.MissingRefs {
			fmt.Println("    " + filepath.Base(ref))
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
