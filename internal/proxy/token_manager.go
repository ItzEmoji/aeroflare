package proxy

import (
	"github.com/itzemoji/aeroflare/pkg/oci"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
)

// TokenManager handles retrieving and caching the OCI Bearer token.
type TokenManager struct {
	registry      string
	repository    string
	githubToken   string
	overrideToken string // Cached environment token

	mu     sync.Mutex
	token  string
	expiry time.Time
	client *http.Client
	now    func() time.Time // clock; overridable in tests
}

// NewTokenManager creates a new OCI token manager.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
	if reg, err := name.NewRegistry(registry); err == nil {
		registry = reg.RegistryStr()
	}

	// Check for a static token override once during initialization. Reject
	// values that look like GitHub/GitLab personal-access tokens (common
	// prefixes below): those are not valid OCI bearer tokens, so a user who
	// pasted one into oci_token/NIXCACHE_TOKEN by mistake falls through to
	// normal token exchange instead of sending an invalid Authorization header.
	var override string
	if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") && !strings.HasPrefix(t, "glpat-") && !strings.HasPrefix(t, "gho_") && !strings.HasPrefix(t, "ghu_") && !strings.HasPrefix(t, "ghs_") {
		override = t
	} else if t := os.Getenv("NIXCACHE_TOKEN"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") && !strings.HasPrefix(t, "glpat-") && !strings.HasPrefix(t, "gho_") && !strings.HasPrefix(t, "ghu_") && !strings.HasPrefix(t, "ghs_") {
		override = t
	}

	return &TokenManager{
		registry:      registry,
		repository:    repository,
		githubToken:   githubToken,
		overrideToken: override,
		client:        &http.Client{Timeout: 10 * time.Second},
		now:           time.Now,
	}
}

// SetOverrideToken sets a static bearer token, bypassing token exchange.
func (tm *TokenManager) SetOverrideToken(token string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.overrideToken = token
}

// GetToken returns a valid OCI Bearer token, performing token exchange if necessary.
func (tm *TokenManager) GetToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// A verbatim bearer token (from oci_token / NIXCACHE_TOKEN) is used as-is.
	// For GHCR this is the base64-encoded PAT, which the registry accepts
	// directly, skipping the /token exchange. NewTokenManager already rejects
	// raw PAT values (ghp_/github_pat_/… prefixes) for this field, so only a
	// properly-formed bearer reaches here.
	if tm.overrideToken != "" {
		return tm.overrideToken, nil
	}

	if tm.token != "" && tm.now().Before(tm.expiry) {
		return tm.token, nil
	}

	token, expiresIn, err := tm.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	// Honor the registry's advertised lifetime with a safety margin so a
	// token never expires mid-request; fall back to 4 minutes when the
	// registry doesn't say (GHCR tokens live ~5 minutes).
	ttl := 4 * time.Minute
	if expiresIn > 0 {
		ttl = time.Duration(expiresIn)*time.Second - 30*time.Second
		if ttl < time.Second {
			ttl = time.Second
		}
	}

	tm.token = token
	tm.expiry = tm.now().Add(ttl)
	return tm.token, nil
}

// fetchToken performs the anonymous (or GitHub-authenticated) OAuth2-style
// token exchange against the registry's /token endpoint and returns the
// bearer token along with its advertised lifetime in seconds (0 if the
// registry didn't report one).
func (tm *TokenManager) fetchToken(ctx context.Context) (string, int, error) {
	scope := fmt.Sprintf("repository:%s:pull", tm.repository)
	proto := oci.GetProtocol(tm.registry)
	tokenURL := fmt.Sprintf("%s://%s/token?scope=%s&service=%s", proto, tm.registry, scope, tm.registry)

	resp, err := oci.DoWithRetry(tm.client, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "aeroflare/1.0")

		// If we have a GitHub auth token, attach it. Otherwise, request anonymously.
		if tm.githubToken != "" {
			req.SetBasicAuth("token", tm.githubToken)
		}
		return req, nil
	})
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("failed to fetch token (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}
	if result.Token == "" {
		return "", 0, fmt.Errorf("empty token returned from registry")
	}

	return result.Token, result.ExpiresIn, nil
}
