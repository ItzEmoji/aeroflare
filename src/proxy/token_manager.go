package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// TokenManager handles retrieving and caching the OCI Bearer token.
type TokenManager struct {
	registry    string
	repository  string
	githubToken string
	mu          sync.Mutex
	token       string
	expiry      time.Time
}

// NewTokenManager creates a new OCI token manager.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
	return &TokenManager{
		registry:    registry,
		repository:  repository,
		githubToken: githubToken,
	}
}

// GetToken returns a valid OCI Bearer token, performing token exchange if necessary.
func (tm *TokenManager) GetToken() (string, error) {
	if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t, nil
	}
	if t := os.Getenv("NIXCACHE_TOKEN"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
		return t, nil
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.token != "" && time.Now().Before(tm.expiry) {
		return tm.token, nil
	}

	token, err := tm.fetchToken()
	if err != nil {
		return "", err
	}

	tm.token = token
	tm.expiry = time.Now().Add(4 * time.Minute) // Cache for 4 minutes
	return tm.token, nil
}

func (tm *TokenManager) fetchToken() (string, error) {
	scope := fmt.Sprintf("repository:%s:pull", tm.repository)
	proto := GetProtocol(tm.registry)
	tokenURL := fmt.Sprintf("%s://%s/token?scope=%s&service=%s", proto, tm.registry, scope, tm.registry)

	client := &http.Client{Timeout: 10 * time.Second}

	if tm.githubToken != "" {
		req, err := http.NewRequest("GET", tokenURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "aeroflare/1.0")
			req.SetBasicAuth("token", tm.githubToken)

			resp, err := client.Do(req)
			if err == nil {
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode == http.StatusOK {
					var result struct {
						Token string `json:"token"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Token != "" {
						return result.Token, nil
					}
				}
			}
		}
	}

	req, err := http.NewRequest("GET", tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch token (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("empty token returned from registry")
	}

	return result.Token, nil
}
