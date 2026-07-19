package auth

import (
	"errors"
	"os"

	"github.com/itzemoji/aeroflare/internal/secrets"
)

// ErrTokenNotFound is returned by Resolve when a credential could not be
// found in any of the configured sources (an explicit flag, environment
// variables, or the secrets manager).
var ErrTokenNotFound = errors.New("token not found in flags, environment, or secrets manager")

// Resolver looks up a single credential (e.g. a GitHub token) by checking,
// in priority order, an explicit flag value, a list of environment
// variables, and finally the secrets manager. Build one with NewResolver and
// the With* methods, then call Resolve.
type Resolver struct {
	secretKey      string
	flagValue      string
	envVars        []string
	secretsManager secrets.Manager
}

// NewResolver creates a Resolver that falls back to the secrets manager
// under secretKey if no flag or environment value is set.
func NewResolver(secretKey string) *Resolver {
	return &Resolver{secretKey: secretKey}
}

// WithSecretsManager overrides the secrets manager used as the final
// fallback (the default is secrets.NewManager()). Primarily used by tests
// to inject a fake manager.
func (r *Resolver) WithSecretsManager(manager secrets.Manager) *Resolver {
	r.secretsManager = manager
	return r
}

// WithFlag sets the highest-priority source: an explicit value, typically
// from a command-line flag. An empty string means "not provided" and falls
// through to the remaining sources.
func (r *Resolver) WithFlag(val string) *Resolver {
	r.flagValue = val
	return r
}

// WithEnv appends environment variable names to check, in the order given,
// after the flag but before the secrets manager. The first variable with a
// non-empty value wins.
func (r *Resolver) WithEnv(keys ...string) *Resolver {
	r.envVars = append(r.envVars, keys...)
	return r
}

// Resolve returns the credential value by checking, in order: the flag
// value, each environment variable, then the secrets manager. It returns
// ErrTokenNotFound if none of the sources yield a value, or any other error
// returned by the secrets manager itself (aside from "not found").
func (r *Resolver) Resolve() (string, error) {
	if r.flagValue != "" {
		return r.flagValue, nil
	}

	for _, key := range r.envVars {
		if val := os.Getenv(key); val != "" {
			return val, nil
		}
	}

	manager := r.secretsManager
	if manager == nil {
		manager = secrets.NewManager()
	}
	val, err := manager.Get(r.secretKey)
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return "", err
	}
	if err == nil && val != "" {
		return val, nil
	}

	return "", ErrTokenNotFound
}

// managerOrDefault returns the first manager in mgrs, or nil (which resolution
// treats as "use the default secrets manager"). It lets the variadic
// convenience resolvers below accept an optional manager override for tests.
func managerOrDefault(mgrs []secrets.Manager) secrets.Manager {
	if len(mgrs) > 0 {
		return mgrs[0]
	}
	return nil
}

// ResolveGithubToken resolves a GitHub credential from (in priority order)
// the GITHUB_TOKEN or GH_TOKEN environment variables, then the secrets
// manager under the "github-token" key. An optional secrets.Manager may be
// passed to override the default (used by tests). The credential's sources are
// declared once, in the catalog (service.go).
func ResolveGithubToken(mgrs ...secrets.Manager) (string, error) {
	return githubService.Fields[0].Resolve(managerOrDefault(mgrs))
}

// ResolveGitlabToken resolves a GitLab credential from the GITLAB_TOKEN
// environment variable, then the secrets manager under the "gitlab-token"
// key. An optional secrets.Manager may be passed to override the default
// (used by tests).
func ResolveGitlabToken(mgrs ...secrets.Manager) (string, error) {
	return gitlabService.Fields[0].Resolve(managerOrDefault(mgrs))
}

// ResolveRegistryToken resolves the token used to authenticate to the given
// OCI registry hostname. The registry-to-service mapping (ghcr.io -> GitHub,
// registry.gitlab.com -> GitLab, anything else -> a generic "oci-<host>-token"
// credential honoring the "oci_token" env var) is defined by the catalog's
// ServiceForRegistry. An optional secrets.Manager overrides the default.
func ResolveRegistryToken(registry string, mgrs ...secrets.Manager) (string, error) {
	svc := ServiceForRegistry(registry)
	tokenField, ok := svc.Field("token")
	if !ok {
		return "", ErrTokenNotFound
	}
	return tokenField.Resolve(managerOrDefault(mgrs))
}
