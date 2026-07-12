// Package set implements `aeroflare auth set`.
package set

import (
	"fmt"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the arguments and dependencies setRun needs, so it can be
// exercised in tests without going through cobra.
type Options struct {
	F    *cmdutil.Factory
	Args []string
}

func NewCmdSet(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	return &cobra.Command{
		Use:   "set <service> [value...]",
		Short: "Save a credential for a known service (github, gitlab, cloudflare, oci <host>)",
		Long: `Save a credential for a service aeroflare understands.

Examples:
  aeroflare auth set github <token>
  aeroflare auth set cloudflare <api-token> <account-id>
  aeroflare auth set oci registry.example.com <username> <token>

With no values, you are prompted for each field (requires a terminal).`,
		Args: cmdutil.FlagErrorArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Args = args
			return setRun(opts)
		},
	}
}

func setRun(opts *Options) error {
	f := opts.F
	manager := f.Secrets()

	svc, values, err := shared.ServiceFromArgs(opts.Args)
	if err != nil {
		return err
	}

	if len(values) > len(svc.Fields) {
		return fmt.Errorf("%s takes at most %d value(s), got %d", svc.DisplayName, len(svc.Fields), len(values))
	}

	// With no positional values, fall back to interactive prompting.
	if len(values) == 0 {
		if !f.IOStreams.IsStdinTTY() {
			return fmt.Errorf("no values provided and not running interactively; pass values as arguments")
		}
		prompted := shared.PromptServiceFields(svc)
		for _, fld := range svc.Fields {
			if v, ok := prompted[fld.Name]; ok && v != "" {
				if err := manager.Set(fld.SecretKey, v); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(f.IOStreams.Out, "Saved %s %s\n", svc.DisplayName, fld.Name)
			}
		}
		return nil
	}

	// Positional values map onto fields in declared order.
	for i, v := range values {
		fld := svc.Fields[i]
		if err := manager.Set(fld.SecretKey, v); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(f.IOStreams.Out, "Saved %s %s\n", svc.DisplayName, fld.Name)
	}

	// Live-validate on save only when interactive, so scripted/test runs
	// stay offline and fast.
	if f.IOStreams.IsStdinTTY() {
		if id, err := shared.ValidateService(svc, manager); err == nil && id != nil {
			printIdentity(f, svc, id)
		}
	}
	return nil
}

// printIdentity renders a validated identity for a service to confirm the
// credential just saved actually works.
func printIdentity(f *cmdutil.Factory, svc auth.Service, id *auth.Identity) {
	if id.User != "" {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "✓ %s authenticated as %s\n", svc.DisplayName, id.User)
	}
	for _, w := range id.Warnings {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "⚠️  %s\n", w)
	}
}
