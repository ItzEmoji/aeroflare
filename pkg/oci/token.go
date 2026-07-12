package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
)

// ExchangeToken performs a token exchange for a given OCI registry.
// Some registries (like ghcr.io) require Basic auth token exchange to get a Bearer token.
// repository should be the full repository path (e.g. "itzemoji/nix-cache-test")
func ExchangeToken(registry, repository, username, basicAuthToken string) (string, error) {
	if reg, err := name.NewRegistry(registry); err == nil {
		registry = reg.RegistryStr()
	}

	proto := GetProtocol(registry)

	// Discover realm and service via /v2/ endpoint
	realm := fmt.Sprintf("%s://%s/token", proto, registry)
	service := registry

	pingReq, _ := http.NewRequest("GET", fmt.Sprintf("%s://%s/v2/", proto, registry), nil)
	pingClient := &http.Client{Timeout: 5 * time.Second}
	if pingResp, err := pingClient.Do(pingReq); err == nil {
		defer func() { _ = pingResp.Body.Close() }()
		if pingResp.StatusCode == 401 {
			authHeader := pingResp.Header.Get("Www-Authenticate")
			if strings.HasPrefix(authHeader, "Bearer ") {
				parts := strings.Split(strings.TrimPrefix(authHeader, "Bearer "), ",")
				for _, part := range parts {
					if strings.HasPrefix(part, "realm=") {
						realm = strings.Trim(strings.TrimPrefix(part, "realm="), "\"")
					} else if strings.HasPrefix(part, "service=") {
						service = strings.Trim(strings.TrimPrefix(part, "service="), "\"")
					}
				}
			}
		}
	}

	scope := fmt.Sprintf("repository:%s:pull,push", repository)

	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("scope", scope)
	q.Set("service", service)
	u.RawQuery = q.Encode()
	tokenURL := u.String()

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}
	if username == "" {
		username = "token"
	}
	req.SetBasicAuth(username, basicAuthToken)
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

