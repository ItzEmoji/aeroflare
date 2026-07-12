package oci

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/itzemoji/aeroflare/internal/auth"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/viper"
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

// GetToken attempts to get a valid token, exchanging a GitHub/GitLab PAT if necessary
func GetToken(registry, repository, explicitToken string) string {
	token := explicitToken
	if token == "" {
		var err error
		token, err = auth.ResolveRegistryToken(registry)
		if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
			fmt.Fprintf(os.Stderr, "Warning: failed to resolve registry token: %v\n", err)
		}
	}

	if token == "" {
		return ""
	}

	username, _ := auth.NewResolver(fmt.Sprintf("oci-%s-username", registry)).Resolve()
	if username == "" {
		username = os.Getenv("AEROFLARE_GIT_USERNAME")
	}

	isGitToken := strings.HasPrefix(token, "ghp_") || strings.HasPrefix(token, "github_pat_") || strings.HasPrefix(token, "glpat-") || strings.HasPrefix(token, "gho_") || strings.HasPrefix(token, "ghu_") || strings.HasPrefix(token, "ghs_")
	isDockerToken := strings.HasPrefix(token, "dckr_pat_")

	// If it's a JWT, or we have no username and it doesn't look like a known PAT, assume it's already a Bearer token.
	if strings.HasPrefix(token, "eyJ") || (!isGitToken && !isDockerToken && username == "") {
		return token
	}

	if username == "" {
		username = "token"
	}

	// Try to exchange it
	exchanged, err := ExchangeToken(registry, repository, username, token)
	if err == nil && exchanged != "" {
		return exchanged
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG ExchangeToken error: %v\n", err)
	}

	return token // Fallback
}

// GetRegistryAndRepository derives the target registry and repository from
// viper config / environment: an explicit cache-url (oci://registry/repo)
// takes precedence, otherwise it falls back to using the cache name as the
// repository. Returns an error if neither is set.
func GetRegistryAndRepository() (string, string, error) {
	registry := viper.GetString("registry")
	if registry == "" {
		registry = os.Getenv("NIXCACHE_REGISTRY")
	}
	if registry == "" {
		registry = "ghcr.io"
	}

	ociURL := viper.GetString("cache-url")
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
		cacheName := viper.GetString("cache")
		if cacheName == "" {
			cacheName = os.Getenv("NIXCACHE_REPO")
		}
		if cacheName == "" {
			return "", "", errors.New("AEROFLARE_CACHE or AEROFLARE_CACHE_URL configuration is required")
		}
		repository = strings.ToLower(cacheName)
	}

	return registry, repository, nil
}
