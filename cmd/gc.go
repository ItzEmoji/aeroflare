package cmd

import (
	"fmt"
	"os"

	network "aeroflare/src"
	"github.com/spf13/cobra"
)

var (
	maxFreed   int64
	printRoots bool
	printLive  bool
	printDead  bool
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage collect the cache",
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()
		ociToken := network.GetToken(registry, repository)
		if ociToken == "" {
			PrintError("oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		PrintInfo("Fetching remote index...")
		index, err := network.FetchCacheIndex(registry, repository, ociToken)
		if err != nil {
			PrintError(fmt.Sprintf("Failed to fetch remote index: %v", err))
			os.Exit(1)
		}

		if printRoots {
			fmt.Println("GC Roots:")
			for _, root := range index.GCRoots {
				fmt.Println("  " + root)
			}
		}

		PrintInfo("Running garbage collection...")
		result := network.RunGC(index, maxFreed)

		if printLive {
			fmt.Println("Live Paths:")
			for _, hash := range result.LivePaths {
				fmt.Println("  " + hash)
			}
		}

		if printDead {
			fmt.Println("Dead Paths:")
			for _, hash := range result.DeadPaths {
				fmt.Println("  " + hash)
			}
		}

		PrintSuccess(fmt.Sprintf("Garbage collection freed %d bytes.", result.FreedBytes))

		if result.FreedBytes > 0 {
			tokenMgr := network.NewTokenManager(registry, repository, "")
			_, configAnnotations, _ := network.BootstrapConfigWithAnnotations(registry, repository, tokenMgr)
			r2Cfg := network.GetR2Config(configAnnotations)

			PrintInfo("Pushing updated remote index...")
			err = network.UpdateCacheIndex(nil, index, registry, repository, ociToken, "", r2Cfg, configAnnotations)
			if err != nil {
				PrintError(fmt.Sprintf("Failed to push updated remote index: %v", err))
				os.Exit(1)
			}
			PrintSuccess("Successfully pushed updated remote index.")
		}
	},
}

func init() {
	gcCmd.Flags().Int64Var(&maxFreed, "max-freed", 0, "Delete at most this number of bytes")
	gcCmd.Flags().BoolVar(&printRoots, "print-roots", false, "Print the GC roots")
	gcCmd.Flags().BoolVar(&printLive, "print-live", false, "Print the live paths")
	gcCmd.Flags().BoolVar(&printDead, "print-dead", false, "Print the dead paths")

	rootCmd.AddCommand(gcCmd)
}
