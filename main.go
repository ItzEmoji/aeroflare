package main

import (
	"fmt"
	"os"
	"strings"

	"aeroflare/src"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <push-blob|pull-blob> <args>\n", os.Args[0])
		os.Exit(1)
	}
	cmd := os.Args[1]

	registry := os.Getenv("NIXCACHE_REGISTRY")
	if registry == "" {
		registry = "ghcr.io"
	}

	repoEnv := os.Getenv("NIXCACHE_REPO")
	if repoEnv == "" {
		fmt.Fprintln(os.Stderr, "Error: NIXCACHE_REPO environment variable is required")
		os.Exit(1)
	}
	repoEnv = strings.ToLower(repoEnv)

	// Ensure the repo includes the nix-cache prefix for standard operation
	repository := fmt.Sprintf("%s/nix-cache", repoEnv)

	ociToken := getToken(registry, repository)
	if ociToken == "" {
		fmt.Fprintln(os.Stderr, "Error: oci_token, GITHUB_TOKEN or GH_TOKEN environment variable is required")
		os.Exit(1)
	}

	switch cmd {
	case "push-blob":
		filePath := os.Args[2]
		digest, err := network.PushBlob(filePath, registry, repository, ociToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to push blob: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(digest)
	case "pull-blob":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s pull-blob <digest> <output-file>\n", os.Args[0])
			os.Exit(1)
		}
		digest := os.Args[2]
		outFile := os.Args[3]
		err := network.PullBlob(digest, outFile, registry, repository, ociToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to pull blob: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Successfully pulled blob to", outFile)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// getToken attempts to get a valid token, exchanging a GitHub PAT if necessary
func getToken(registry, repository string) string {
	if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t // Token seems to be a valid Bearer token already
	}

	cred := os.Getenv("GITHUB_TOKEN")
	if cred == "" {
		cred = os.Getenv("GH_TOKEN")
	}
	if cred == "" {
		return os.Getenv("oci_token")
	}

	// Try to exchange it
	exchanged, err := network.ExchangeToken(registry, repository, cred)
	if err == nil && exchanged != "" {
		return exchanged
	}

	return cred // Fallback
}
