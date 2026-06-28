package cmd

import (
	"fmt"
	"os"

	network "aeroflare/src"
	"aeroflare/src/push"
	"aeroflare/src/run"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [--] <command>...",
	Short: "Run a command with proxy substituter and push the output paths",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()
		indexDir := getIndexDir(repository)

		cfg := &run.RunConfig{
			Command: args,
		}

		run.DisplaySummary(cfg)

		targetPaths, err := run.ExecuteCommand(cfg, registry, repository, indexDir, getGithubToken())
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		if len(targetPaths) == 0 {
			PrintWarning("No nix store paths found in command stdout. Nothing to push.")
			return
		}

		fmt.Printf("\nFound %d store paths to push from run command output.\n", len(targetPaths))

		// Trigger Push
		pushCfg := &push.PushConfig{
			TargetPaths: targetPaths,
			Compression: pushCompression,
			CacheURL:    pushCacheURL,
			Workers:     pushWorkers,
			PrepareRefs: pushPrepareRefs,
			SigningKey:  pushSigningKey,
			KeepFiles:   pushKeepFiles,
			ForcePush:   pushForcePush,
			Verbosity:   VerboseCount,
		}

		plan, err := push.Preflight(pushCfg)
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		push.DisplaySummary(plan)

		if err := push.RunPush(plan); err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}
	},
}

func init() {
	// Re-use push flags
	runCmd.Flags().StringVar(&pushCompression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	runCmd.Flags().StringVar(&pushCacheURL, "cache-url", "https://cache.nixos.org", "Upstream binary cache URL")
	runCmd.Flags().IntVar(&pushWorkers, "workers", 50, "Number of concurrent workers")
	runCmd.Flags().BoolVar(&pushPrepareRefs, "prepare-refs", true, "Also prepare references")
	runCmd.Flags().StringVar(&pushSigningKey, "signing-key", "", "Path to Nix signing private key file")
	runCmd.Flags().BoolVar(&pushKeepFiles, "keep", false, "Keep generated files")
	runCmd.Flags().BoolVar(&pushForcePush, "force", false, "Force push files")

	rootCmd.AddCommand(runCmd)
}
