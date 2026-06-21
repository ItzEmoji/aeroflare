package network

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ExchangeToken performs a token exchange for a given OCI registry.
// Some registries (like ghcr.io) require Basic auth token exchange to get a Bearer token.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func ExchangeToken(registry, repository, basicAuthToken string) (string, error) {
	scope := fmt.Sprintf("repository:%s:pull,push", repository)
	proto := GetProtocol(registry)
	tokenURL := fmt.Sprintf("%s://%s/token?scope=%s&service=%s", proto, registry, scope, registry)

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth("token", basicAuthToken)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Token != "" {
			return result.Token, nil
		}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	return "", fmt.Errorf("failed to exchange token (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
}

// GetToken attempts to get a valid token, exchanging a GitHub PAT if necessary
func GetToken(registry, repository string) string {
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
	exchanged, err := ExchangeToken(registry, repository, cred)
	if err == nil && exchanged != "" {
		return exchanged
	}

	return cred // Fallback
}

// GetRegistryAndRepository computes the registry and repository from environment variables.
func GetRegistryAndRepository() (string, string) {
	registry := os.Getenv("AEROFLARE_REGISTRY")
	if registry == "" {
		registry = os.Getenv("NIXCACHE_REGISTRY")
	}
	if registry == "" {
		registry = "ghcr.io"
	}

	ociURL := os.Getenv("AEROFLARE_OCI_URL")
	var repository string

	if ociURL != "" {
		ociURL = strings.TrimPrefix(ociURL, "oci://")
		parts := strings.SplitN(ociURL, "/", 2)
		if len(parts) == 2 && strings.Contains(parts[0], ".") {
			registry = parts[0]
			repository = parts[1]
		} else {
			repository = ociURL
		}
	} else {
		cacheName := os.Getenv("AEROFLARE_CACHE")
		if cacheName == "" {
			cacheName = os.Getenv("NIXCACHE_REPO")
		}
		if cacheName == "" {
			fmt.Fprintln(os.Stderr, "Error: AEROFLARE_CACHE or AEROFLARE_OCI_URL environment variable is required")
			os.Exit(1)
		}
		cacheName = strings.ToLower(cacheName)
		repository = fmt.Sprintf("%s/nix-cache", cacheName)
	}

	return registry, repository
}
