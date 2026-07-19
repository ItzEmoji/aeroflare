// Package shared holds token-resolution and interactive-auth logic used by
// the auth command and by every other command that needs a credential
// (init, run, push, blob, proxy, configure). It replaces the package-global
// SecretsManager and globalGithubToken/etc. of the old cmd package with a
// *cmdutil.Factory passed explicitly, and turns the old os.Exit(1) failure
// path into a returned error.
package shared

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/internal/secrets"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
)

// ServiceFromArgs maps positional CLI args to a catalog service. The first arg
// is the service name; the special name "oci" consumes a second arg as the
// registry host. It returns the resolved service, the remaining positional
// args (field values for `set`, an optional field name for `get`), and an
// error naming the valid services if the name is not recognized.
func ServiceFromArgs(args []string) (auth.Service, []string, error) {
	name := args[0]
	if name == "oci" {
		if len(args) < 2 || args[1] == "" {
			return auth.Service{}, nil, fmt.Errorf("usage: oci <host> [username] [token]")
		}
		return auth.ServiceForRegistry(args[1]), args[2:], nil
	}
	svc, ok := auth.ServiceByID(name)
	if !ok {
		return auth.Service{}, nil, fmt.Errorf("unknown service %q (valid: github, gitlab, cloudflare, or oci <host>)", name)
	}
	return svc, args[1:], nil
}

// Redact masks a secret value for display, revealing only its last few
// characters so a credential can be recognized without being exposed.
func Redact(val string) string {
	if len(val) <= 4 {
		return "****"
	}
	return "****" + val[len(val)-4:]
}

// ValidateService resolves a service's fields and runs its live validation
// check, returning the authenticated identity. It returns (nil, nil) when the
// service declares no validation.
func ValidateService(svc auth.Service, m secrets.Manager) (*auth.Identity, error) {
	if svc.Validate == nil {
		return nil, nil
	}
	vals, err := svc.Resolve(m)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return svc.Validate(ctx, vals)
}

// RequireGithubToken resolves a GitHub token from (in order) the --github-token
// flag, the secrets manager/environment, or an interactive device/manual auth
// flow if stdin is a terminal. Returns an error if no token can be obtained
// non-interactively.
func RequireGithubToken(f *cmdutil.Factory) (string, error) {
	if f.Overrides.GithubToken != "" {
		return f.Overrides.GithubToken, nil
	}

	token, err := auth.ResolveGithubToken(f.Secrets())
	if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
		_, _ = fmt.Fprintf(f.IOStreams.ErrOut, "Warning: failed to read token from keychain: %v\n", err)
	}
	if token != "" {
		return token, nil
	}

	if f.IOStreams.IsStdinTTY() {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "GitHub token is required but not found. Launching authentication...")
		return RunInteractiveGithubAuth(f)
	}

	return "", errors.New("GitHub token required, please set GITHUB_TOKEN or run 'aeroflare auth login'")
}

// RequireGitlabToken resolves a GitLab token the same way RequireGithubToken
// resolves a GitHub token: flag, then secrets manager/environment, then an
// interactive prompt if possible.
func RequireGitlabToken(f *cmdutil.Factory) (string, error) {
	if f.Overrides.GitlabToken != "" {
		return f.Overrides.GitlabToken, nil
	}

	token, err := auth.ResolveGitlabToken(f.Secrets())
	if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
		_, _ = fmt.Fprintf(f.IOStreams.ErrOut, "Warning: failed to read token from keychain: %v\n", err)
	}
	if token != "" {
		return token, nil
	}

	if f.IOStreams.IsStdinTTY() {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "GitLab token is required but not found. Launching authentication...")
		return RunInteractiveGitlabAuth(f)
	}

	return "", errors.New("GitLab token required, please set GITLAB_TOKEN or run 'aeroflare auth login'")
}

// RequireCloudflareToken resolves the Cloudflare API token and account ID,
// each independently from a flag, secrets manager, or matching env var
// (CLOUDFLARE_API_TOKEN / CLOUDFLARE_ACCOUNT_ID). If either is still missing
// it falls back to an interactive prompt, or errors if not on a terminal.
func RequireCloudflareToken(f *cmdutil.Factory) (string, string, error) {
	cf, _ := auth.ServiceByID("cloudflare")

	apiToken := f.Overrides.CfToken
	if apiToken == "" {
		apiToken = ResolveField(f, cf, "token")
	}

	accountID := f.Overrides.CfAccountID
	if accountID == "" {
		accountID = ResolveField(f, cf, "account_id")
	}

	if apiToken != "" && accountID != "" {
		return apiToken, accountID, nil
	}

	if f.IOStreams.IsStdinTTY() {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "Cloudflare credentials required but incomplete. Launching authentication...")
		return RunInteractiveCloudflareAuth(f)
	}

	return "", "", errors.New("cloudflare credentials required, please set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID, or run 'aeroflare auth login'")
}

// GetOCIToken looks up a saved username/token pair for an arbitrary OCI
// registry, without prompting. Returns empty strings for whichever half is
// not found; use RequireOCIToken if an interactive fallback is wanted.
func GetOCIToken(f *cmdutil.Factory, registry string) (string, string) {
	svc := auth.ServiceForRegistry(registry)
	return ResolveField(f, svc, "username"), ResolveField(f, svc, "token")
}

// ResolveField resolves a single named field of a service from the environment
// and the secrets manager, returning "" if it is unset. A read error other
// than "not found" is reported to stderr, matching the tolerant behavior the
// callers above rely on. Fields the service does not declare resolve to "".
func ResolveField(f *cmdutil.Factory, svc auth.Service, name string) string {
	field, ok := svc.Field(name)
	if !ok {
		return ""
	}
	val, err := field.Resolve(f.Secrets())
	if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
		_, _ = fmt.Fprintf(f.IOStreams.ErrOut, "Warning: failed to read credential from keychain: %v\n", err)
	}
	return val
}

// RequireOCIToken returns a username/token pair for registry, prompting
// interactively (via RunInteractiveOCIAuth) if credentials aren't already
// saved. Errors if credentials are missing and stdin isn't a terminal.
func RequireOCIToken(f *cmdutil.Factory, registry string) (string, string, error) {
	user, pass := GetOCIToken(f, registry)
	if user != "" && pass != "" {
		return user, pass, nil
	}

	if f.IOStreams.IsStdinTTY() {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "Credentials for registry %s are required. Launching authentication...\n", registry)
		return RunInteractiveOCIAuth(f, registry)
	}

	return "", "", fmt.Errorf("credentials required for registry %s, run 'aeroflare auth login' to set them", registry)
}

// TokenForRegistry resolves (prompting interactively if needed) and exports
// the token for registry into the process environment (oci_token, plus
// GITHUB_TOKEN for ghcr.io) so downstream Nix/OCI tooling can pick it up.
func TokenForRegistry(f *cmdutil.Factory, registry string) (string, error) {
	if registry == "ghcr.io" {
		token, err := RequireGithubToken(f)
		if err != nil {
			return "", err
		}
		_ = os.Setenv("oci_token", token)
		_ = os.Setenv("GITHUB_TOKEN", token)
		return token, nil
	} else if registry != "" {
		_, token, err := RequireOCIToken(f, registry)
		if err != nil {
			return "", err
		}
		_ = os.Setenv("oci_token", token)
		return token, nil
	}
	return "", nil
}

// OptionalTokenForRegistry is like TokenForRegistry but never prompts or
// fails: it returns "" if no token is already saved, which lets the proxy
// serve unauthenticated (public) registries without forcing a login.
func OptionalTokenForRegistry(f *cmdutil.Factory, registry string) string {
	if registry == "" {
		return ""
	}
	token, _ := auth.ResolveRegistryToken(registry, f.Secrets())
	if token != "" {
		_ = os.Setenv("oci_token", token)
		if registry == "ghcr.io" {
			_ = os.Setenv("GITHUB_TOKEN", token)
		}
	}
	return token
}
