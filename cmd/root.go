package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "aeroflare",
	Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
	Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		PrintError(err.Error())
		os.Exit(1)
	}
}

func init() {
	// Root command flags can be added here
}

func getGithubToken() string {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return token
}
