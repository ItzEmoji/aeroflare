package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"io"
	"net/http"
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
	overrideToken string // verbatim bearer supplied by the caller

	mu     sync.Mutex
	token  string
	expiry time.Time
	client *http.Client
	now    func() time.Time // clock; overridable in tests
}

// patPrefixes are the personal-access-token prefixes used by GitHub and GitLab.
// A PAT is not a valid OCI bearer token.
var patPrefixes = []string{"ghp_", "github_pat_", "glpat-", "gho_", "ghu_", "ghs_"}

// IsBearerToken reports whether token can be sent verbatim as an OCI bearer
// credential. It rejects raw GitHub/GitLab personal-access tokens: a user who
// pastes one where a bearer is expected should fall through to normal token
// exchange rather than have an invalid Authorization header sent for them.
func IsBearerToken(token string) bool {
	if token == "" {
		return false
	}
	for _, p := range patPrefixes {
		if strings.HasPrefix(token, p) {
			return false
		}
	}
	return true
}

// NewTokenManager creates a new OCI token manager.
//
// It does not read the environment. A caller that wants to supply a verbatim
// bearer token (as the CLI does from oci_token / NIXCACHE_TOKEN) passes it to
// SetOverrideToken.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
	if reg, err := name.NewRegistry(registry); err == nil {
		registry = reg.RegistryStr()
	}

	return &TokenManager{
		registry:    registry,
		repository:  repository,
		githubToken: githubToken,
		client:      &http.Client{Timeout: 10 * time.Second},
		now:         time.Now,
	}
}

// SetOverrideToken sets a static bearer token, bypassing token exchange.
// Values that are not usable bearer tokens (see IsBearerToken) are ignored, so
// a mistakenly-supplied PAT falls through to normal exchange.
func (tm *TokenManager) SetOverrideToken(token string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if !IsBearerToken(token) {
		tm.overrideToken = ""
		return
	}
	tm.overrideToken = token
}

// GetToken returns a valid OCI Bearer token, performing token exchange if necessary.
func (tm *TokenManager) GetToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// A verbatim bearer token supplied via SetOverrideToken is used as-is. For
	// GHCR this is the base64-encoded PAT, which the registry accepts directly,
	// skipping the /token exchange. SetOverrideToken rejects raw PAT values
	// (ghp_/github_pat_/… prefixes), so only a properly-formed bearer reaches
	// here.
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
