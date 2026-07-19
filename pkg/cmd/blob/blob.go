// Package blob implements the `push-blob` and `pull-blob` commands, which push
// or pull a single blob to/from the OCI registry directly (bypassing the Nix
// store/push pipeline).
package blob

import (
	"errors"
	"fmt"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/itzemoji/aeroflare/pkg/oci"

	"github.com/spf13/cobra"
)

// PushOptions holds the flags and dependencies push-blob needs.
type PushOptions struct {
	IO *iostreams.IOStreams

	FilePath string
}

// NewCmdPushBlob builds the `aeroflare push-blob` command.
func NewCmdPushBlob(f *cmdutil.Factory) *cobra.Command {
	opts := &PushOptions{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "push-blob [file-path]",
		Short: "Push a blob to the registry",
		Args:  cmdutil.FlagErrorArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.FilePath = args[0]
			return pushBlobRun(opts)
		},
	}

	return cmd
}

func pushBlobRun(opts *PushOptions) error {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}

	auth := cmdutil.RegistryAuth(registry, "")
	if auth == nil {
		return errors.New("no registry credential found: set oci_token, GITHUB_TOKEN or GH_TOKEN, or run aeroflare auth login")
	}

	opts.IO.Info(fmt.Sprintf("Pushing blob: %s", opts.FilePath))

	digest, err := oci.PushBlob(opts.FilePath, registry, repository, auth)
	if err != nil {
		return fmt.Errorf("failed to push blob: %w", err)
	}

	_, _ = fmt.Fprintf(opts.IO.Out, "✔ Blob Digest: %s\n", digest)

	return nil
}

// PullOptions holds the flags and dependencies pull-blob needs.
type PullOptions struct {
	IO *iostreams.IOStreams

	Digest  string
	OutFile string
}

// NewCmdPullBlob builds the `aeroflare pull-blob` command.
func NewCmdPullBlob(f *cmdutil.Factory) *cobra.Command {
	opts := &PullOptions{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "pull-blob [digest] [output-file]",
		Short: "Pull a blob from the registry",
		Args:  cmdutil.FlagErrorArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Digest = args[0]
			opts.OutFile = args[1]
			return pullBlobRun(opts)
		},
	}

	return cmd
}

func pullBlobRun(opts *PullOptions) error {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}

	auth := cmdutil.RegistryAuth(registry, "")
	if auth == nil {
		return errors.New("no registry credential found: set oci_token, GITHUB_TOKEN or GH_TOKEN, or run aeroflare auth login")
	}

	opts.IO.Info(fmt.Sprintf("Pulling blob %s to %s", opts.Digest, opts.OutFile))

	if err := oci.PullBlob(opts.Digest, opts.OutFile, registry, repository, auth); err != nil {
		return fmt.Errorf("failed to pull blob: %w", err)
	}

	opts.IO.Success(fmt.Sprintf("Successfully pulled blob to %s", opts.OutFile))

	return nil
}
