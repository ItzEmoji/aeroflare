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
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "aeroflare version %s\n", build.Version)
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
