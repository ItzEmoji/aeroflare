// Package get implements `aeroflare auth get`.
package get

import (
	"fmt"

	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the arguments and dependencies getRun needs, so it can be
// exercised in tests without going through cobra.
type Options struct {
	F    *cmdutil.Factory
	Args []string
}

func NewCmdGet(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	return &cobra.Command{
		Use:   "get <service> [field]",
		Short: "Print a stored credential value (for scripting)",
		Long: `Print the raw stored value of a credential to stdout, for use in scripts.

Examples:
  aeroflare auth get github
  aeroflare auth get cloudflare account_id
  aeroflare auth get oci registry.example.com token

For a multi-field service with no field given, each field is printed as
"name=value".`,
		Args: cmdutil.FlagErrorArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Args = args
			return getRun(opts)
		},
	}
}

func getRun(opts *Options) error {
	f := opts.F
	manager := f.Secrets()

	svc, rest, err := shared.ServiceFromArgs(opts.Args)
	if err != nil {
		return err
	}

	// A single trailing positional selects one field by name.
	if len(rest) == 1 {
		field, ok := svc.Field(rest[0])
		if !ok {
			return fmt.Errorf("service %s has no field %q", svc.DisplayName, rest[0])
		}
		val, err := field.Resolve(manager)
		if err != nil {
			return fmt.Errorf("no value found for %s %s: %w", svc.DisplayName, field.Name, err)
		}
		_, err = fmt.Fprintln(f.IOStreams.Out, val)
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("get takes at most one field name")
	}

	vals, err := svc.Resolve(manager)
	if err != nil {
		return err
	}
	if len(vals) == 0 {
		return fmt.Errorf("no credentials stored for %s", svc.DisplayName)
	}

	// Single-field service: print the bare value so it can be piped.
	if len(svc.Fields) == 1 {
		_, err = fmt.Fprintln(f.IOStreams.Out, vals[svc.Fields[0].Name])
		return err
	}

	// Multi-field service: print each field as name=value, in declared order.
	for _, fld := range svc.Fields {
		if v, ok := vals[fld.Name]; ok {
			if _, err := fmt.Fprintf(f.IOStreams.Out, "%s=%s\n", fld.Name, v); err != nil {
				return err
			}
		}
	}
	return nil
}
