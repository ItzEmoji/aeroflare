package cmd

import (
	"os"

	network "aeroflare/src"
	"github.com/spf13/cobra"
)

var VerboseCount int

var rootCmd = &cobra.Command{
	Use:   "aeroflare",
	Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
	Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		network.DebugLogger = (VerboseCount >= 2)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		PrintError(err.Error())
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().CountVarP(&VerboseCount, "verbose", "v", "Enable verbose output (-v for packages, -vv for requests)")
}

func getGithubToken() string {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return token
}
