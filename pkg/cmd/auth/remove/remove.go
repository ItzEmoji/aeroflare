// Package remove implements `aeroflare auth remove`.
package remove

import (
	"fmt"

	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the arguments and dependencies removeRun needs, so it can be
// exercised in tests without going through cobra.
type Options struct {
	F    *cmdutil.Factory
	Args []string
}

func NewCmdRemove(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	return &cobra.Command{
		Use:   "remove <service>",
		Short: "Remove a stored credential (github, gitlab, cloudflare, oci <host>)",
		Long: `Delete every field of a service's credential from the secrets store.

Examples:
  aeroflare auth remove github
  aeroflare auth remove oci registry.example.com`,
		Args: cmdutil.FlagErrorArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Args = args
			return removeRun(opts)
		},
	}
}

func removeRun(opts *Options) error {
	f := opts.F
	manager := f.Secrets()

	svc, _, err := shared.ServiceFromArgs(opts.Args)
	if err != nil {
		return err
	}

	removed := 0
	for _, fld := range svc.Fields {
		// Only delete fields that are actually stored, so the summary count
		// is accurate.
		if _, gerr := manager.Get(fld.SecretKey); gerr != nil {
			continue
		}
		if err := manager.Delete(fld.SecretKey); err != nil {
			return err
		}
		removed++
	}

	if removed == 0 {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "No stored credentials for %s.\n", svc.DisplayName)
	} else {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "Removed %s credentials.\n", svc.DisplayName)
	}
	return nil
}
