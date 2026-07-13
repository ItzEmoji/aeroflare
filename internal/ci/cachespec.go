// Package ci implements aeroflare-ci: a lightweight, non-interactive CI runner
// that builds Nix flake installables and pushes them to one or more OCI caches.
package ci

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/itzemoji/aeroflare/pkg/oci"
)

// CacheSpec is a single push destination parsed from "<registry>;<repository>".
type CacheSpec struct {
	Registry   string // e.g. "ghcr.io"
	Repository string // e.g. "itzemoji/nix-cache"
	Raw        string // original entry, for messages
}

// ParseCacheSpec parses a "<registry>;<repository>" entry.
func ParseCacheSpec(s string) (CacheSpec, error) {
	raw := strings.TrimSpace(s)
	parts := strings.SplitN(raw, ";", 2)
	if len(parts) != 2 {
		return CacheSpec{}, fmt.Errorf("invalid cache %q: expected <registry>;<repository>", s)
	}
	reg := strings.TrimSpace(parts[0])
	reg = strings.TrimPrefix(reg, "https://")
	reg = strings.TrimPrefix(reg, "http://")
	repo := strings.TrimSpace(parts[1])
	if reg == "" || repo == "" {
		return CacheSpec{}, fmt.Errorf("invalid cache %q: registry and repository must be non-empty", s)
	}
	return CacheSpec{Registry: reg, Repository: repo, Raw: reg + ";" + repo}, nil
}

// envVarFor returns an env var name for a registry host, e.g.
// ("AEROFLARE_TOKEN_", "ghcr.io") -> "AEROFLARE_TOKEN_GHCR_IO".
func envVarFor(prefix, registry string) string {
	up := strings.ToUpper(registry)
	up = strings.NewReplacer(".", "_", ":", "_").Replace(up)
	return prefix + up
}

// TokenEnvVar returns the token env var name for a registry host, e.g.
// "ghcr.io" -> "AEROFLARE_TOKEN_GHCR_IO".
func TokenEnvVar(registry string) string { return envVarFor("AEROFLARE_TOKEN_", registry) }

// UsernameEnvVar returns the username env var name for a registry host, e.g.
// "docker.io" -> "AEROFLARE_USERNAME_DOCKER_IO".
func UsernameEnvVar(registry string) string { return envVarFor("AEROFLARE_USERNAME_", registry) }

// ResolveToken resolves the push token for a registry host from the environment.
// The generic AEROFLARE_TOKEN_<HOST> override wins; otherwise a per-host default
// applies (ghcr.io -> GITHUB_TOKEN). Returns "" if nothing is set.
func ResolveToken(registry string) string {
	if v := os.Getenv(TokenEnvVar(registry)); v != "" {
		return v
	}
	switch registry {
	case "ghcr.io":
		return os.Getenv("GITHUB_TOKEN")
	}
	return ""
}

// ResolveAuth resolves a registry credential from the environment, as the
// credential it is: a password to be exchanged with the registry, not a bearer
// token to be presented.
//
// This distinction is the whole bug the Action used to have. GitHub Actions
// hands out a GITHUB_TOKEN of the form "ghs_...", which is a personal access
// token. It was being passed as push.Target.Token -- documented as "a
// ready-to-use registry bearer token" -- and so was sent to ghcr.io as
// "Authorization: Bearer ghs_...", which ghcr.io rejects. The push subcommand
// escaped this only because it happened to route its token through an exchange
// first. There is no longer a field that means "bearer", so the mistake has
// nowhere left to live.
//
// The username comes from AEROFLARE_USERNAME_<HOST>, falling back to the
// registry-agnostic AEROFLARE_GIT_USERNAME. It matters to registries that check
// it -- GitLab wants "gitlab-ci-token" for a job token, Docker Hub wants the
// real account name -- and is ignored by ghcr.io. Returns nil when no token is
// set.
func ResolveAuth(registry string) authn.Authenticator {
	token := ResolveToken(registry)
	if token == "" {
		return nil
	}

	username := os.Getenv(UsernameEnvVar(registry))
	if username == "" {
		username = os.Getenv("AEROFLARE_GIT_USERNAME")
	}
	return oci.PasswordAuth(username, token)
}
