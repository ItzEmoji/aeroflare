package cmd

import (
	"fmt"

	"github.com/itzemoji/aeroflare/internal/build"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of aeroflare",
	RunE: func(cmd *cobra.Command, args []string) error {
		dateStr := ""
		if build.Date != "" {
			dateStr = fmt.Sprintf(" (%s)", build.Date)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "aeroflare version %s%s\n", build.Version, dateStr)
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
