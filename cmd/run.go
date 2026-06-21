package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	network "aeroflare/src"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [--] <command>...",
	Short: "Run a command with proxy substituter and push the output paths",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		indexDir := getIndexDir(repository)

		// Start proxy on random port in background
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		port, err := network.StartProxy(ctx, 0, "127.0.0.1", registry, repository, indexDir, 300, []string{"https://cache.nixos.org"}, getGithubToken(), "")
		if err != nil {
			PrintError(fmt.Sprintf("Failed to start proxy: %v", err))
			os.Exit(1)
		}

		PrintInfo(fmt.Sprintf("Started background proxy on 127.0.0.1:%d", port))

		var stdoutBuf bytes.Buffer
		cmdToRun := exec.Command(args[0], args[1:]...)
		cmdToRun.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		cmdToRun.Stderr = os.Stderr
		cmdToRun.Stdin = os.Stdin

		// Add substituter to NIX_CONFIG
		env := os.Environ()
		nixConfig := os.Getenv("NIX_CONFIG")
		if nixConfig != "" {
			nixConfig += "\n"
		}
		nixConfig += fmt.Sprintf("extra-substituters = http://127.0.0.1:%d", port)
		env = append(env, "NIX_CONFIG="+nixConfig)
		cmdToRun.Env = env

		err = cmdToRun.Run()
		if err != nil {
			PrintError(fmt.Sprintf("Command failed: %v", err))
			os.Exit(1)
		}

		var targetPaths []string
		lines := strings.Split(stdoutBuf.String(), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && strings.HasPrefix(line, "/nix/store/") {
				targetPaths = append(targetPaths, line)
			}
		}

		if len(targetPaths) == 0 {
			PrintWarning("No nix store paths found in command stdout. Nothing to push.")
			return
		}

		fmt.Printf("\nFound %d store paths to push.\n", len(targetPaths))

		// Push
		performPush(targetPaths)
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
