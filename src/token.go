package network

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ExchangeToken performs a token exchange for a given OCI registry.
// Some registries (like ghcr.io) require Basic auth token exchange to get a Bearer token.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test/nix-cache")
func ExchangeToken(registry, repository, basicAuthToken string) (string, error) {
	scope := fmt.Sprintf("repository:%s:pull,push", repository)
	tokenURL := fmt.Sprintf("https://%s/token?scope=%s&service=%s", registry, scope, registry)

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
