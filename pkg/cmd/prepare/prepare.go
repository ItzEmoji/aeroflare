// Package prepare implements `aeroflare prepare`, which generates NAR
// archives and narinfo files from Nix store paths without pushing them
// anywhere.
package prepare

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/itzemoji/aeroflare/pkg/prepare/compress"
	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
	"github.com/itzemoji/aeroflare/pkg/prepare/signing"

	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies prepareRun needs.
type Options struct {
	IO *iostreams.IOStreams

	StorePath     string
	InputFile     string
	OutputDir     string
	Compression   string
	Workers       int
	PrepareRefs   bool
	SigningKey    string
	UpstreamCache string
}

// NewCmdPrepare builds the `aeroflare prepare` command.
func NewCmdPrepare(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Generate NAR archives and narinfo files from Nix store paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			return prepareRun(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.StorePath, "store-path", "", "Nix store path to prepare (e.g. /nix/store/xxx-yyy)")
	cmd.Flags().StringVar(&opts.InputFile, "input", "", "File containing store paths (one per line, # for comments)")
	cmd.Flags().StringVar(&opts.OutputDir, "output-dir", "./output", "Output directory for .nar and .narinfo files")
	cmd.Flags().StringVar(&opts.Compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	cmd.Flags().IntVar(&opts.Workers, "workers", 50, "Number of concurrent workers")
	cmd.Flags().BoolVar(&opts.PrepareRefs, "prepare-refs", false, "Also prepare NAR+narinfo for references not on the upstream cache (one level deep)")
	cmd.Flags().StringVar(&opts.SigningKey, "signing-key", "", "Path to Nix signing private key file (format: name:base64seed, as produced by 'nix key-gen-secret')")
	cmd.Flags().StringVar(&opts.UpstreamCache, "upstream-cache", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")

	return cmd
}

func prepareRun(cmd *cobra.Command, opts *Options) error {
	if opts.StorePath == "" && opts.InputFile == "" {
		_ = cmd.Usage()
		return errors.New("--store-path or --input is required")
	}

	compType, err := compress.ParseType(opts.Compression)
	if err != nil {
		return err
	}

	var signKey *signing.PrivateKey
	if opts.SigningKey != "" {
		signKey, err = signing.LoadPrivateKey(opts.SigningKey)
		if err != nil {
			return fmt.Errorf("error loading signing key: %w", err)
		}
	}

	var upstreamURLs []string
	if opts.UpstreamCache != "" {
		upstreamURLs = []string{opts.UpstreamCache}
	}

	cfg := &prepare.Config{
		OutputDir:          opts.OutputDir,
		Compression:        compType,
		CacheURLs:          upstreamURLs,
		Workers:            opts.Workers,
		PrepareMissingRefs: opts.PrepareRefs,
		SigningKey:         signKey,
	}

	ctx := context.Background()

	if opts.StorePath != "" {
		opts.IO.Info(fmt.Sprintf("Preparing store path: %s", opts.StorePath))
		result, err := prepare.Prepare(ctx, opts.StorePath, cfg)
		if err != nil {
			return err
		}
		opts.printResult(result)
	} else {
		paths, err := prepare.ParseInputFile(opts.InputFile)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return errors.New("no store paths found in input file")
		}

		opts.IO.Info(fmt.Sprintf("Preparing %d paths from input file...", len(paths)))
		results, err := prepare.PrepareBatch(ctx, paths, cfg)
		if err != nil {
			return err
		}

		totalMissing := 0
		totalPreparedRefs := 0
		totalSigned := 0
		for _, result := range results {
			opts.printResult(result)
			totalMissing += len(result.MissingRefs)
			totalPreparedRefs += len(result.MissingRefResults)
			if result.Signed {
				totalSigned++
			}
		}

		summary := fmt.Sprintf("Processed %d paths, %d missing references, %d refs prepared, %d signed", len(results), totalMissing, totalPreparedRefs, totalSigned)
		opts.IO.Success(summary)
	}

	return nil
}

// printResult prints a human-readable summary of one prepared store path:
// its NAR/narinfo output locations, signing status, and any references
// that were missing from (or newly prepared for) the upstream cache.
func (opts *Options) printResult(r *prepare.Result) {
	_, _ = fmt.Fprintln(opts.IO.Out, "Prepared: "+r.StorePath)
	_, _ = fmt.Fprintln(opts.IO.Out, "  NAR:     "+r.NarPath)
	_, _ = fmt.Fprintln(opts.IO.Out, "  Narinfo: "+r.NarinfoPath)
	if r.Signed {
		_, _ = fmt.Fprintln(opts.IO.Out, "  Signed:  yes")
	}
	if len(r.MissingRefs) > 0 {
		_, _ = fmt.Fprintf(opts.IO.Out, "  Missing references (%d, not on upstream cache):\n", len(r.MissingRefs))
		for _, ref := range r.MissingRefs {
			_, _ = fmt.Fprintln(opts.IO.Out, "    "+filepath.Base(ref))
		}
		if len(r.MissingRefResults) > 0 {
			_, _ = fmt.Fprintf(opts.IO.Out, "  Prepared missing refs (%d):\n", len(r.MissingRefResults))
			for _, rr := range r.MissingRefResults {
				_, _ = fmt.Fprintf(opts.IO.Out, "    %s -> %s, %s\n", filepath.Base(rr.StorePath), filepath.Base(rr.NarPath), filepath.Base(rr.NarinfoPath))
			}
		}
	} else if len(r.References) > 0 {
		_, _ = fmt.Fprintf(opts.IO.Out, "  All %d references found on upstream cache\n", len(r.References))
	}
}
