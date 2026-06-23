package proxy

import (
        "context"
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
        registry      string
        repository    string
        githubToken   string
        overrideToken string // Cached environment token
        
        mu     sync.Mutex
        token  string
        expiry time.Time
        client *http.Client
}

// NewTokenManager creates a new OCI token manager.
func NewTokenManager(registry, repository, githubToken string) *TokenManager {
        // Check for static token overrides once during initialization
        var override string
        if t := os.Getenv("oci_token"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
                override = t
        } else if t := os.Getenv("NIXCACHE_TOKEN"); t != "" && !strings.HasPrefix(t, "ghp_") && !strings.HasPrefix(t, "github_pat_") {
                override = t
        }

        return &TokenManager{
                registry:      registry,
                repository:    repository,
                githubToken:   githubToken,
                overrideToken: override,
                client:        &http.Client{Timeout: 10 * time.Second},
        }
}

// GetToken returns a valid OCI Bearer token, performing token exchange if necessary.
func (tm *TokenManager) GetToken(ctx context.Context) (string, error) {
        if tm.overrideToken != "" {
                return tm.overrideToken, nil
        }

        tm.mu.Lock()
        defer tm.mu.Unlock()

        if tm.token != "" && time.Now().Before(tm.expiry) {
                return tm.token, nil
        }

        token, err := tm.fetchToken(ctx)
        if err != nil {
                return "", err
        }

        tm.token = token
        tm.expiry = time.Now().Add(4 * time.Minute) // Cache for 4 minutes
        return tm.token, nil
}

func (tm *TokenManager) fetchToken(ctx context.Context) (string, error) {
        scope := fmt.Sprintf("repository:%s:pull", tm.repository)
        proto := GetProtocol(tm.registry)
        tokenURL := fmt.Sprintf("%s://%s/token?scope=%s&service=%s", proto, tm.registry, scope, tm.registry)

        req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
        if err != nil {
                return "", err
        }
        req.Header.Set("User-Agent", "aeroflare/1.0")

        // If we have a GitHub auth token, attach it. Otherwise, request anonymously.
        if tm.githubToken != "" {
                req.SetBasicAuth("token", tm.githubToken)
        }

        resp, err := tm.client.Do(req)
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
