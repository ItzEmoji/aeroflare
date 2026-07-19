package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/itzemoji/aeroflare/internal/secrets"
)

// Field is one atomic secret that makes up a credential. A Service is composed
// of one or more Fields (e.g. GitHub has a single token field, while Cloudflare
// has separate API-token and account-ID fields).
type Field struct {
	// Name is the stable, machine identifier for the field within its service
	// ("token", "account_id", "username"). It is what `auth get <svc> <field>`
	// accepts and what Service.Resolve keys its result map by.
	Name string
	// Label is the human-facing description used in prompts and status output
	// ("Cloudflare Account ID").
	Label string
	// SecretKey is the key this field is stored under in the secrets manager
	// ("github-token", "cf-account-id", "oci-docker.io-token").
	SecretKey string
	// EnvVars lists environment variables checked before the secrets manager,
	// highest priority first.
	EnvVars []string
	// Secret reports whether the value is sensitive: masked on input and
	// redacted in status/display output.
	Secret bool
	// Optional reports whether the service can still be considered usable when
	// this field is absent.
	Optional bool
}

// Resolve returns the field's value, checking its environment variables in
// order and then the secrets manager. It returns ErrTokenNotFound if the field
// is set in none of them.
func (f Field) Resolve(m secrets.Manager) (string, error) {
	return NewResolver(f.SecretKey).
		WithEnv(f.EnvVars...).
		WithSecretsManager(m).
		Resolve()
}

// Identity is the result of a live credential validation: who the credential
// authenticates as, plus any non-fatal warnings (e.g. missing OAuth scopes).
type Identity struct {
	User     string
	Warnings []string
}

// ValidateFunc verifies that a resolved credential actually works. vals maps
// field Name to value. It returns the authenticated identity, or an error if
// the credential is invalid or the check could not be performed.
type ValidateFunc func(ctx context.Context, vals map[string]string) (*Identity, error)

// Service is a credential kind aeroflare understands. The catalog of services
// is the single source of truth: resolution, display, prompting, import, and
// validation all derive from these declarations rather than re-encoding key
// names and env vars independently.
type Service struct {
	// ID is the stable service identifier ("github", "gitlab", "cloudflare",
	// or "oci:<host>" for a generic OCI registry).
	ID string
	// DisplayName is the human-facing service name ("GitHub").
	DisplayName string
	// Fields are the atomic secrets that make up this credential.
	Fields []Field
	// Validate performs a live check of the credential, or is nil if the
	// service supports no validation.
	Validate ValidateFunc
}

// Field returns the field with the given Name and whether it exists.
func (s Service) Field(name string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Resolve returns the values of every field that could be resolved, keyed by
// field Name. Fields that are set in neither the environment nor the secrets
// manager are simply omitted; a non-"not found" error from the manager is
// returned as-is.
func (s Service) Resolve(m secrets.Manager) (map[string]string, error) {
	vals := make(map[string]string)
	for _, f := range s.Fields {
		val, err := f.Resolve(m)
		if errors.Is(err, ErrTokenNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if val != "" {
			vals[f.Name] = val
		}
	}
	return vals, nil
}

// githubService, gitlabService, and cloudflareService are the fixed catalog
// entries. Their Validate funcs are wired up in validate.go.
var githubService = Service{
	ID:          "github",
	DisplayName: "GitHub",
	Fields: []Field{
		{
			Name:      "token",
			Label:     "GitHub Token",
			SecretKey: "github-token",
			EnvVars:   []string{"GITHUB_TOKEN", "GH_TOKEN"},
			Secret:    true,
		},
	},
	// Validate wired in validate.go
}

var gitlabService = Service{
	ID:          "gitlab",
	DisplayName: "GitLab",
	Fields: []Field{
		{
			Name:      "token",
			Label:     "GitLab Personal Access Token",
			SecretKey: "gitlab-token",
			EnvVars:   []string{"GITLAB_TOKEN"},
			Secret:    true,
		},
	},
	// Validate wired in validate.go
}

var cloudflareService = Service{
	ID:          "cloudflare",
	DisplayName: "Cloudflare",
	Fields: []Field{
		{
			Name:      "token",
			Label:     "Cloudflare API Token",
			SecretKey: "cf-token",
			EnvVars:   []string{"CLOUDFLARE_API_TOKEN"},
			Secret:    true,
		},
		{
			Name:      "account_id",
			Label:     "Cloudflare Account ID",
			SecretKey: "cf-account-id",
			EnvVars:   []string{"CLOUDFLARE_ACCOUNT_ID"},
		},
	},
	// Validate wired in validate.go
}

// fixedServices is the catalog of services with statically-known keys. OCI
// registries are handled separately by ServiceForRegistry because their keys
// depend on the registry hostname.
var fixedServices = []Service{githubService, gitlabService, cloudflareService}

// Services returns the fixed catalog of credential services (GitHub, GitLab,
// Cloudflare). Generic OCI registries are not included because they are keyed
// dynamically by hostname; use ServiceForRegistry for those.
func Services() []Service {
	out := make([]Service, len(fixedServices))
	copy(out, fixedServices)
	return out
}

// ServiceByID returns the fixed service with the given ID and whether it
// exists. It does not resolve dynamic "oci:<host>" IDs; use ServiceForRegistry
// for OCI registries.
func ServiceByID(id string) (Service, bool) {
	for _, s := range fixedServices {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

// ServiceForRegistry returns the service used to authenticate to an OCI
// registry. Well-known registries alias onto a fixed service (ghcr.io ->
// GitHub, registry.gitlab.com -> GitLab); any other host yields a generic
// service keyed "oci-<host>-username" / "oci-<host>-token".
func ServiceForRegistry(host string) Service {
	switch host {
	case "ghcr.io":
		return githubService
	case "registry.gitlab.com":
		return gitlabService
	}
	return genericOCIService(host)
}

// genericOCIService builds the username/token service for an arbitrary OCI
// registry hostname.
func genericOCIService(host string) Service {
	return Service{
		ID:          "oci:" + host,
		DisplayName: "OCI Registry (" + host + ")",
		Fields: []Field{
			{
				Name:      "username",
				Label:     "Username for " + host,
				SecretKey: fmt.Sprintf("oci-%s-username", host),
			},
			{
				Name:      "token",
				Label:     "Token / Password for " + host,
				SecretKey: fmt.Sprintf("oci-%s-token", host),
				EnvVars:   []string{"oci_token"},
				Secret:    true,
			},
		},
	}
}

// ServiceForSecretKey maps a stored secret key back to the service that owns
// it, and whether such a service exists. It recognizes the fixed services'
// keys and the "oci-<host>-username" / "oci-<host>-token" pattern. Keys that
// belong to no known service (e.g. legacy arbitrary keys) return ok=false.
func ServiceForSecretKey(key string) (Service, bool) {
	for _, s := range fixedServices {
		for _, f := range s.Fields {
			if f.SecretKey == key {
				return s, true
			}
		}
	}
	if host, ok := ociHostFromKey(key); ok {
		return ServiceForRegistry(host), true
	}
	return Service{}, false
}

// ociHostFromKey extracts the registry hostname from an "oci-<host>-username"
// or "oci-<host>-token" secret key. The hostname itself may contain hyphens
// and dots, so only the fixed "oci-" prefix and "-username"/"-token" suffix
// are stripped.
func ociHostFromKey(key string) (string, bool) {
	if !strings.HasPrefix(key, "oci-") {
		return "", false
	}
	for _, suffix := range []string{"-username", "-token"} {
		if strings.HasSuffix(key, suffix) {
			host := strings.TrimSuffix(strings.TrimPrefix(key, "oci-"), suffix)
			if host != "" {
				return host, true
			}
		}
	}
	return "", false
}
