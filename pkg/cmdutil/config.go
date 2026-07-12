package cmdutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/proxy"

	"github.com/spf13/viper"
)

// Registry and token resolution lives here, in the CLI layer, rather than in
// pkg/oci. pkg/oci takes registry, repository, and token as explicit
// parameters so that it can be embedded, tested, and driven with more than one
// set of credentials in a single process. Reaching into viper, the environment,
// and the OS keyring is a command-line concern, so it belongs on this side of
// the boundary.

// RegistryAndRepository derives the target registry and repository from viper
// config and the environment. An explicit cache-url (oci://registry/repo) wins;
// otherwise the configured cache name becomes the repository and the registry
// defaults to ghcr.io. Returns an error if neither is configured.
func RegistryAndRepository() (string, string, error) {
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

// RegistryOverrideToken returns a verbatim registry bearer token from the
// environment (oci_token, then NIXCACHE_TOKEN), or "" if neither is set to a
// usable bearer. For GHCR this is the base64-encoded PAT, which the registry
// accepts directly, skipping token exchange.
//
// The proxy and push packages take this as a parameter rather than reading the
// environment themselves; this is the CLI-side lookup that feeds them.
func RegistryOverrideToken() string {
	for _, key := range []string{"oci_token", "NIXCACHE_TOKEN"} {
		if t := os.Getenv(key); proxy.IsBearerToken(t) {
			return t
		}
	}
	return ""
}

// RegistryToken resolves a usable registry credential, exchanging a GitHub or
// GitLab PAT for a short-lived bearer token when the registry requires it. It
// falls back to the raw token if the exchange fails, and returns "" when no
// credential can be found at all.
//
// explicitToken, when non-empty, takes precedence over the keyring.
//
// Registry bearer tokens expire. Callers holding one for a long operation
// should call this again to refresh rather than caching the result; see
// push.Target.TokenSource.
func RegistryToken(registry, repository, explicitToken string) string {
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

	exchanged, err := oci.ExchangeToken(registry, repository, username, token)
	if err == nil && exchanged != "" {
		return exchanged
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG ExchangeToken error: %v\n", err)
	}

	return token // Fallback
}
