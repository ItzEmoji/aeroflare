// Package version implements `aeroflare version`, reporting the build version
// and date stamped into the binary at link time.
package version

import (
	"fmt"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/spf13/cobra"
)

func NewCmdVersion(f *cmdutil.Factory, version, buildDate string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number of aeroflare",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(f.IOStreams.Out, Format(version, buildDate))
			return err
		},
	}
}

// Format renders the version line, appending the build date in parentheses
// when one was baked in via ldflags.
func Format(version, buildDate string) string {
	dateStr := ""
	if buildDate != "" {
		dateStr = fmt.Sprintf(" (%s)", buildDate)
	}
	return fmt.Sprintf("aeroflare version %s%s\n", version, dateStr)
}
