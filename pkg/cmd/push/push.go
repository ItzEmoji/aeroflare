// Package push implements `aeroflare push` and provides the shared flag set
// and pipeline (Preflight -> DisplaySummary -> RunPush) that `run` reuses so
// the two commands cannot drift apart.
package push

import (
	"os"

	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	internalpush "github.com/itzemoji/aeroflare/pkg/push"

	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies push and run need to build a
// PushConfig and drive the shared push pipeline.
type Options struct {
	IO *iostreams.IOStreams

	StorePath string
	InputFile string

	Compression   string
	UpstreamCache string
	Workers       int
	PrepareRefs   bool
	SigningKey    string
	KeepFiles     bool
	ForcePush     bool
}

// AddPushFlags registers the flags that `push` and `run` share, so the two
// commands cannot drift apart. Previously both bound to the same package-level
// vars; this makes the shared contract explicit and compiler-checked.
func AddPushFlags(cmd *cobra.Command, opts *Options) {
	cmd.Flags().StringVar(&opts.Compression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	cmd.Flags().StringVar(&opts.UpstreamCache, "upstream-cache", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	cmd.Flags().IntVar(&opts.Workers, "workers", 50, "Number of concurrent workers")
	cmd.Flags().BoolVar(&opts.PrepareRefs, "prepare-refs", true, "Also prepare references that are not on the upstream cache")
	cmd.Flags().StringVar(&opts.SigningKey, "signing-key", "", "Path to Nix signing private key file")
	cmd.Flags().BoolVar(&opts.KeepFiles, "keep", false, "Keep generated .nar and .narinfo files after the push")
	cmd.Flags().BoolVar(&opts.ForcePush, "force", false, "Force push files even if they exist in the index or upstream cache")
}

// NewCmdPush builds the `aeroflare push` command.
func NewCmdPush(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "push [installable...]",
		Short: "Push a build to the cache",
		Long: `Push builds to the OCI cache.

Each positional argument is a Nix installable, resolved to store paths before
uploading:

  - a store path       (/nix/store/xxx-yyy)
  - a result symlink   (./result)
  - a flake reference  (github:owner/repo, nixpkgs#hello, .#default)

Installables that are not yet built are built first. Store paths given via
--store-path, --input, or piped stdin are taken literally.

Examples:
  aeroflare push ./result
  aeroflare push nixpkgs#hello
  aeroflare push github:owner/repo#default`,
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
	registry, _, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}
	// Called for its side effect: resolves and exports the registry token
	// (oci_token / GITHUB_TOKEN) into the environment for downstream push steps.
	if _, err := shared.TokenForRegistry(f, registry); err != nil {
		return err
	}

	// Positional args are Nix installables (store paths, result symlinks, or
	// flake refs like github:owner/repo or nixpkgs#hello); resolve them to
	// realized store paths, building any that are not yet in the store.
	storePaths, err := newInstallableResolver().resolve(args)
	if err != nil {
		return err
	}

	cfg, err := internalpush.ParseConfig(storePaths, opts.StorePath, opts.InputFile, os.Stdin)
	if err != nil {
		return err
	}

	cfg.Compression = opts.Compression
	cfg.CacheURL = opts.UpstreamCache
	cfg.Workers = opts.Workers
	cfg.PrepareRefs = opts.PrepareRefs
	cfg.SigningKey = opts.SigningKey
	cfg.KeepFiles = opts.KeepFiles
	cfg.ForcePush = opts.ForcePush
	cfg.Verbosity = f.Overrides.Verbose

	return Run(f, opts, cfg)
}

// Run drives the shared push pipeline: Preflight -> summary -> RunPushTo.
// It is the extracted, identical tail of both `push` and `run`.
func Run(f *cmdutil.Factory, opts *Options, cfg *internalpush.PushConfig) error {
	target, err := Target()
	if err != nil {
		return err
	}

	plan, err := internalpush.Preflight(cfg)
	if err != nil {
		return err
	}

	// The command layer owns presentation: it renders the summary and hands the
	// pipeline a reporter, rather than the pipeline printing for itself.
	reporter := NewUIReporter(cfg.Verbosity >= 1)
	reporter.Summary("Push Summary", internalpush.SummaryFields(plan))

	_, err = internalpush.RunPushTo(plan, target, reporter)
	return err
}

// Target resolves the push destination from CLI config: registry and repository
// from viper/env, and the registry credential from the flag, environment or
// keyring. The credential refreshes itself inside the transport, so a long push
// no longer has to re-resolve one as it goes.
func Target() (internalpush.Target, error) {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return internalpush.Target{}, err
	}

	return internalpush.Target{
		Registry:   registry,
		Repository: repository,
		Auth:       cmdutil.RegistryAuth(registry, ""),
	}, nil
}
