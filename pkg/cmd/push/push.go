// Package push implements `aeroflare push` and provides the shared flag set
// and pipeline (Preflight -> DisplaySummary -> RunPush) that `run` reuses so
// the two commands cannot drift apart.
package push

import (
	"os"

	"github.com/itzemoji/aeroflare/internal/oci"
	internalpush "github.com/itzemoji/aeroflare/internal/push"
	"github.com/itzemoji/aeroflare/internal/secrets"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies push and run need to build a
// PushConfig and drive the shared push pipeline.
type Options struct {
	IO      *iostreams.IOStreams
	Secrets func() secrets.Manager

	StorePath string
	InputFile string

	Compression string
	CacheURL    string
	Workers     int
	PrepareRefs bool
	SigningKey  string
	KeepFiles   bool
	ForcePush   bool

	Verbosity int
}

// AddPushFlags registers the flags that `push` and `run` share, so the two
// commands cannot drift apart. Previously both bound to the same package-level
// vars; this makes the shared contract explicit and compiler-checked.
func AddPushFlags(cmd *cobra.Command, opts *Options) {
	cmd.Flags().StringVar(&opts.Compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	cmd.Flags().StringVar(&opts.CacheURL, "upstream-cache", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	cmd.Flags().IntVar(&opts.Workers, "workers", 50, "Number of concurrent workers")
	cmd.Flags().BoolVar(&opts.PrepareRefs, "prepare-refs", true, "Also prepare references that are not on the upstream cache")
	cmd.Flags().StringVar(&opts.SigningKey, "signing-key", "", "Path to Nix signing private key file")
	cmd.Flags().BoolVar(&opts.KeepFiles, "keep", false, "Keep generated .nar and .narinfo files after the push")
	cmd.Flags().BoolVar(&opts.ForcePush, "force", false, "Force push files even if they exist in the index or upstream cache")
}

// NewCmdPush builds the `aeroflare push` command.
func NewCmdPush(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO:      f.IOStreams,
		Secrets: f.Secrets,
	}

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push a build to the cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return pushRun(f, opts, args)
		},
	}

	cmd.Flags().StringVar(&opts.StorePath, "store-path", "", "Nix store path to prepare and push (e.g. /nix/store/xxx-yyy)")
	cmd.Flags().StringVar(&opts.InputFile, "input", "", "File containing store paths (one per line, # for comments)")
	AddPushFlags(cmd, opts)

	return cmd
}

func pushRun(f *cmdutil.Factory, opts *Options, args []string) error {
	registry, _ := oci.GetRegistryAndRepository()
	// Called for its side effect: resolves and exports the registry token
	// (oci_token / GITHUB_TOKEN) into the environment for downstream push steps.
	if _, err := shared.TokenForRegistry(f, registry); err != nil {
		return err
	}

	cfg, err := internalpush.ParseConfig(args, opts.StorePath, opts.InputFile, os.Stdin)
	if err != nil {
		return err
	}

	cfg.Compression = opts.Compression
	cfg.CacheURL = opts.CacheURL
	cfg.Workers = opts.Workers
	cfg.PrepareRefs = opts.PrepareRefs
	cfg.SigningKey = opts.SigningKey
	cfg.KeepFiles = opts.KeepFiles
	cfg.ForcePush = opts.ForcePush
	cfg.Verbosity = f.Overrides.Verbose

	return Run(f, opts, cfg)
}

// Run drives the shared push pipeline: Preflight -> DisplaySummary -> RunPush.
// It is the extracted, identical tail of both `push` and `run`.
func Run(f *cmdutil.Factory, opts *Options, cfg *internalpush.PushConfig) error {
	plan, err := internalpush.Preflight(cfg)
	if err != nil {
		return err
	}

	internalpush.DisplaySummary(plan)

	return internalpush.RunPush(plan)
}
