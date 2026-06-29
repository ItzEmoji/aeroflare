package cmd

import (
	"fmt"
	"os"

	network "aeroflare/src"
	"github.com/spf13/cobra"
)

var pushBlobCmd = &cobra.Command{
	Use:   "push-blob [file-path]",
	Short: "Push a blob to the registry",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		ociToken := network.GetToken(registry, repository, "")
		if ociToken == "" {
			PrintError("oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		filePath := args[0]
		PrintInfo(fmt.Sprintf("Pushing blob: %s", filePath))

		digest, err := network.PushBlob(filePath, registry, repository, ociToken)
		if err != nil {
			PrintError(fmt.Sprintf("Failed to push blob: %v", err))
			os.Exit(1)
		}

		fmt.Printf("✔ Blob Digest: %s\n", digest)
	},
}

var pullBlobCmd = &cobra.Command{
	Use:   "pull-blob [digest] [output-file]",
	Short: "Pull a blob from the registry",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		ociToken := network.GetToken(registry, repository, "")
		if ociToken == "" {
			PrintError("oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
			os.Exit(1)
		}

		digest := args[0]
		outFile := args[1]

		PrintInfo(fmt.Sprintf("Pulling blob %s to %s", digest, outFile))

		err := network.PullBlob(digest, outFile, registry, repository, ociToken)
		if err != nil {
			PrintError(fmt.Sprintf("Failed to pull blob: %v", err))
			os.Exit(1)
		}
		PrintSuccess(fmt.Sprintf("Successfully pulled blob to %s", outFile))
	},
}

func init() {
	rootCmd.AddCommand(pushBlobCmd)
	rootCmd.AddCommand(pullBlobCmd)
}
