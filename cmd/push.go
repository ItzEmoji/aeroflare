package cmd

import (
	"os"

	network "aeroflare/src"
	"aeroflare/src/push"
	"github.com/spf13/cobra"
)

var (
	pushStorePath   string
	pushInputFile   string
	pushCompression string
	pushCacheURL    string
	pushWorkers     int
	pushPrepareRefs bool
	pushSigningKey  string
	pushKeepFiles   bool
	pushForcePush   bool
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push a build to the cache",
	Run: func(cmd *cobra.Command, args []string) {
		registry, _ := network.GetRegistryAndRepository()
		getTokenForRegistry(registry)

		cfg, err := push.ParseConfig(args, pushStorePath, pushInputFile, os.Stdin)
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		cfg.Compression = pushCompression
		cfg.CacheURL = pushCacheURL
		cfg.Workers = pushWorkers
		cfg.PrepareRefs = pushPrepareRefs
		cfg.SigningKey = pushSigningKey
		cfg.KeepFiles = pushKeepFiles
		cfg.ForcePush = pushForcePush
		cfg.Verbosity = VerboseCount

		plan, err := push.Preflight(cfg)
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
	pushCmd.Flags().StringVar(&pushStorePath, "store-path", "", "Nix store path to prepare and push (e.g. /nix/store/xxx-yyy)")
	pushCmd.Flags().StringVar(&pushInputFile, "input", "", "File containing store paths (one per line, # for comments)")
	pushCmd.Flags().StringVar(&pushCompression, "compression", "zstd", "Compression type: zstd, xz, gzip, none")
	pushCmd.Flags().StringVar(&pushCacheURL, "upstream-cache", "https://cache.nixos.org", "Upstream binary cache URL (empty to skip reference checking)")
	pushCmd.Flags().IntVar(&pushWorkers, "workers", 50, "Number of concurrent workers")
	pushCmd.Flags().BoolVar(&pushPrepareRefs, "prepare-refs", true, "Also prepare references that are not on the upstream cache")
	pushCmd.Flags().StringVar(&pushSigningKey, "signing-key", "", "Path to Nix signing private key file")
	pushCmd.Flags().BoolVar(&pushKeepFiles, "keep", false, "Keep generated .nar and .narinfo files after the push")
	pushCmd.Flags().BoolVar(&pushForcePush, "force", false, "Force push files even if they exist in the index or upstream cache")

	rootCmd.AddCommand(pushCmd)
}
