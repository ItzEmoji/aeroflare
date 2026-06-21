package cmd

import (
	"fmt"
	"os"
	"strings"

	network "aeroflare/src"
	"github.com/spf13/cobra"
)

var cleanIndexCmd = &cobra.Command{
	Use:   "clean-index",
	Short: "Completely wipe the remote index on the registry",
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		fmt.Print("Are you sure you want to completely wipe the remote index on the registry? [y/N]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			PrintInfo("Aborted.")
			return
		}

		ociToken := network.GetToken(registry, repository)
		if ociToken == "" {
			PrintError("oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		emptyIndex := &network.PushCacheIndex{
			Entries: make(map[string]network.PushCacheEntry),
			GCRoots: []string{},
		}

		tokenMgr := network.NewTokenManager(registry, repository, "")
		_, configAnnotations, _ := network.BootstrapConfigWithAnnotations(registry, repository, tokenMgr)
		r2Cfg := network.GetR2Config(configAnnotations)

		PrintInfo("Wiping remote index...")
		err := network.UpdateCacheIndex(nil, emptyIndex, registry, repository, ociToken, "", r2Cfg, configAnnotations)
		if err != nil {
			PrintError(fmt.Sprintf("Failed to wipe remote index: %v", err))
			os.Exit(1)
		}
		PrintSuccess("Successfully wiped remote index.")
	},
}

func init() {
	rootCmd.AddCommand(cleanIndexCmd)
}
