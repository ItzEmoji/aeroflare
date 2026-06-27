package cmd

import (
	"context"
	"fmt"
	"os"

	network "aeroflare/src"
	"aeroflare/src/proxy"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactively configure cache backend and settings",
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()
		ociToken := network.GetToken(registry, repository)
		if ociToken == "" {
			PrintError("Authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
			os.Exit(1)
		}

		// Fetch existing config manifest if we can
		var existingBackend = "cache-index.json (Not Recommended)"
		var existingPublicKey = ""
		var existingR2Bucket = ""
		var existingR2PublicURL string
		var existingR2Endpoint = ""

		tokenMgr := proxy.NewTokenManager(registry, repository, getGithubToken())
		remoteConf, existingAnnotations, _ := proxy.BootstrapConfigWithAnnotations(context.Background(), nil, registry, repository, tokenMgr)
		if existingAnnotations != nil {
			b := existingAnnotations["aeroflare.index-type"]
			if b == "" {
				b = existingAnnotations["aeroflare.backend"]
			}
			if b != "" {
				switch b {
				case "r2":
					existingBackend = "Cloudflare R2 (Recommended)"
				case "native":
					existingBackend = "Native OCI Tags (Experimental)"
				default:
					existingBackend = "cache-index.json (Not Recommended)"
				}
			}
			if pk := existingAnnotations["aeroflare.public-key"]; pk != "" {
				existingPublicKey = pk
			} else if pk := existingAnnotations["public-key"]; pk != "" {
				existingPublicKey = pk
			}
			if b := existingAnnotations["aeroflare.r2.bucket"]; b != "" {
				existingR2Bucket = b
			}
			if b := existingAnnotations["public-r2-url"]; b != "" {
				existingR2PublicURL = b
			} else if b := existingAnnotations["aeroflare.r2.public_url"]; b != "" {
				existingR2PublicURL = b
			}
			if b := existingAnnotations["aeroflare.r2.endpoint"]; b != "" {
				existingR2Endpoint = b
			}
		} else if remoteConf != nil && remoteConf.PublicKey != "" {
			existingPublicKey = remoteConf.PublicKey
		}

		var backend string
		var publicKey string
		var r2Bucket string
		var r2PublicURL string
		var r2Endpoint string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Choose your cache backend (index-type)").
					Options(
						huh.NewOption("Cloudflare R2 (Recommended)", "r2"),
						huh.NewOption("Native OCI Tags (Experimental)", "native"),
						huh.NewOption("cache-index.json (Not Recommended)", "json"),
					).
					Value(&backend),
				huh.NewInput().
					Title("Public Key").
					Description("Enter your nix cache public key (optional)").
					Value(&publicKey),
			),
		)

		switch existingBackend {
		case "Cloudflare R2 (Recommended)":
			backend = "r2"
		case "Native OCI Tags (Experimental)":
			backend = "native"
		default:
			backend = "json"
		}
		publicKey = existingPublicKey

		err := form.Run()
		if err != nil {
			if err.Error() != "user aborted" {
				PrintError(fmt.Sprintf("Form error: %v", err))
			}
			os.Exit(1)
		}

		if backend == "r2" {
			r2Bucket = existingR2Bucket
			r2PublicURL = existingR2PublicURL
			r2Endpoint = existingR2Endpoint

			r2Form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("R2 Bucket Name").Value(&r2Bucket),
					huh.NewInput().Title("R2 Public URL (e.g., https://pub-xxx.r2.dev)").Value(&r2PublicURL),
					huh.NewInput().Title("R2 S3 API Endpoint").Value(&r2Endpoint),
				),
			)
			err = r2Form.Run()
			if err != nil {
				if err.Error() != "user aborted" {
					PrintError(fmt.Sprintf("Form error: %v", err))
				}
				os.Exit(1)
			}
		}

		annotations := map[string]string{
			"aeroflare.index-type": backend,
			"aeroflare.public-key": publicKey,
		}

		if backend == "r2" {
			annotations["aeroflare.r2.bucket"] = r2Bucket
			annotations["public-r2-url"] = r2PublicURL
			annotations["aeroflare.r2.endpoint"] = r2Endpoint
			// DO NOT save access_key and secret_key to public OCI annotations!
		}

		PrintInfo("Saving configuration to OCI manifest annotations...")

		err = network.PushConfigManifest(registry, repository, ociToken, annotations)
		if err != nil {
			PrintError(fmt.Sprintf("Failed to save config: %v", err))
			os.Exit(1)
		}

		if backend != "json" {
			// If switching away from json backend, try to untag cache-index image
			_ = network.DeleteTag("cache-index", registry, repository, ociToken)
		}

		PrintSuccess("Configuration successfully saved to cache-config manifest!")
	},
}

func init() {
	rootCmd.AddCommand(configureCmd)
}
