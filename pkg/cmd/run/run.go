// Package run implements `aeroflare run`.
package run

import (
	"fmt"

	nixrun "github.com/itzemoji/aeroflare/internal/run"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmd/push"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	internalpush "github.com/itzemoji/aeroflare/pkg/push"

	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies runRun needs, so it can be
// exercised in tests without going through cobra.
type Options struct {
	Push push.Options
}

// NewCmdRun builds the `aeroflare run` command. It reuses push's shared flags
// and pipeline (via push.AddPushFlags and push.Run) so `run` and `push`
// cannot drift apart.
func NewCmdRun(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		Push: push.Options{
			IO: f.IOStreams,
		},
	}

	cmd := &cobra.Command{
		Use:   "run [--] <command>...",
		Short: "Run a command with proxy substituter and push the output paths",
		Args:  cmdutil.FlagErrorArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(f, opts, args)
		},
	}

	// These flags bind to opts.Push, the same struct push.Run consumes, so
	// `aeroflare run` and `aeroflare push` share identical push behavior.
	push.AddPushFlags(cmd, &opts.Push)

	return cmd
}

func runRun(f *cmdutil.Factory, opts *Options, args []string) error {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}

	cfg := &nixrun.RunConfig{
		Command: args,
	}

	nixrun.DisplaySummary(cfg)

	token, err := shared.TokenForRegistry(f, registry)
	if err != nil {
		return err
	}

	targetPaths, err := nixrun.ExecuteCommand(cfg, registry, repository, token)
	if err != nil {
		return err
	}

	if len(targetPaths) == 0 {
		f.IOStreams.Warning("No nix store paths found in command stdout. Nothing to push.")
		return nil
	}

	_, _ = fmt.Fprintf(f.IOStreams.Out, "\nFound %d store paths to push from run command output.\n", len(targetPaths))

	// Feed the discovered store paths straight into the push pipeline,
	// reusing push's own flags (registered above) for compression, workers, etc.
	pushCfg := &internalpush.PushConfig{
		TargetPaths: targetPaths,
		Compression: opts.Push.Compression,
		CacheURL:    opts.Push.UpstreamCache,
		Workers:     opts.Push.Workers,
		PrepareRefs: opts.Push.PrepareRefs,
		SigningKey:  opts.Push.SigningKey,
		KeepFiles:   opts.Push.KeepFiles,
		ForcePush:   opts.Push.ForcePush,
		Verbosity:   f.Overrides.Verbose,
	}

	return push.Run(f, &opts.Push, pushCfg)
}
