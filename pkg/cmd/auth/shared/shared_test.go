package shared

import (
	"os"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

func TestRequireGithubTokenPrefersFlagOverride(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "from-keychain"})
	f.Overrides.GithubToken = "from-flag"

	got, err := RequireGithubToken(f)
	if err != nil {
		t.Fatalf("RequireGithubToken() error = %v", err)
	}
	if got != "from-flag" {
		t.Errorf("RequireGithubToken() = %q, want %q", got, "from-flag")
	}
}

func TestRequireGithubTokenFallsBackToSecretsManager(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "from-keychain"})

	got, err := RequireGithubToken(f)
	if err != nil {
		t.Fatalf("RequireGithubToken() error = %v", err)
	}
	if got != "from-keychain" {
		t.Errorf("RequireGithubToken() = %q, want %q", got, "from-keychain")
	}
}

func TestRequireGithubTokenEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-gh-token")
	t.Setenv("GH_TOKEN", "")
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	got, err := RequireGithubToken(f)
	if err != nil {
		t.Fatalf("RequireGithubToken() error = %v", err)
	}
	if got != "env-gh-token" {
		t.Errorf("RequireGithubToken() = %q, want %q", got, "env-gh-token")
	}
}

// This case was impossible to test before: the old code called os.Exit(1).
// NewTestFactory reports stdin as non-interactive, so the interactive fallback
// is skipped and the error path is reached.
func TestRequireGithubTokenErrorsWhenNotATerminal(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	_, err := RequireGithubToken(f)
	if err == nil {
		t.Fatal("RequireGithubToken() = nil error, want an error when no token and not a TTY")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error %q should tell the user to set GITHUB_TOKEN", err)
	}
}

func TestRequireGitlabTokenPrefersFlagOverride(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"gitlab-token": "from-keychain"})
	f.Overrides.GitlabToken = "from-flag"

	got, err := RequireGitlabToken(f)
	if err != nil {
		t.Fatalf("RequireGitlabToken() error = %v", err)
	}
	if got != "from-flag" {
		t.Errorf("RequireGitlabToken() = %q, want %q", got, "from-flag")
	}
}

func TestRequireGitlabTokenFallsBackToSecretsManager(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"gitlab-token": "secret-gl-token"})

	got, err := RequireGitlabToken(f)
	if err != nil {
		t.Fatalf("RequireGitlabToken() error = %v", err)
	}
	if got != "secret-gl-token" {
		t.Errorf("RequireGitlabToken() = %q, want %q", got, "secret-gl-token")
	}
}

func TestRequireGitlabTokenEnv(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "env-gl-token")
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	got, err := RequireGitlabToken(f)
	if err != nil {
		t.Fatalf("RequireGitlabToken() error = %v", err)
	}
	if got != "env-gl-token" {
		t.Errorf("RequireGitlabToken() = %q, want %q", got, "env-gl-token")
	}
}

func TestRequireGitlabTokenErrorsWhenNotATerminal(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	_, err := RequireGitlabToken(f)
	if err == nil {
		t.Fatal("RequireGitlabToken() = nil error, want an error when no token and not a TTY")
	}
	if !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Errorf("error %q should tell the user to set GITLAB_TOKEN", err)
	}
}

func TestRequireCloudflareTokenGlobalOverride(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)
	f.Overrides.CfToken = "global-cf-token"
	f.Overrides.CfAccountID = "global-cf-user"

	token, user, err := RequireCloudflareToken(f)
	if err != nil {
		t.Fatalf("RequireCloudflareToken() error = %v", err)
	}
	if token != "global-cf-token" || user != "global-cf-user" {
		t.Errorf("RequireCloudflareToken() = (%q, %q), want (%q, %q)", token, user, "global-cf-token", "global-cf-user")
	}
}

func TestRequireCloudflareTokenFallsBackToSecretsManager(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{
		"cf-token":      "secret-cf-token",
		"cf-account-id": "secret-cf-user",
	})

	token, user, err := RequireCloudflareToken(f)
	if err != nil {
		t.Fatalf("RequireCloudflareToken() error = %v", err)
	}
	if token != "secret-cf-token" || user != "secret-cf-user" {
		t.Errorf("RequireCloudflareToken() = (%q, %q), want (%q, %q)", token, user, "secret-cf-token", "secret-cf-user")
	}
}

func TestRequireCloudflareTokenEnv(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "env-cf-token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "env-cf-user")
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	token, user, err := RequireCloudflareToken(f)
	if err != nil {
		t.Fatalf("RequireCloudflareToken() error = %v", err)
	}
	if token != "env-cf-token" || user != "env-cf-user" {
		t.Errorf("RequireCloudflareToken() = (%q, %q), want (%q, %q)", token, user, "env-cf-token", "env-cf-user")
	}
}

func TestRequireCloudflareTokenErrorsWhenNotATerminal(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	_, _, err := RequireCloudflareToken(f)
	if err == nil {
		t.Fatal("RequireCloudflareToken() = nil error, want an error when incomplete and not a TTY")
	}
	if !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
		t.Errorf("error %q should tell the user to set CLOUDFLARE_API_TOKEN", err)
	}
}

func TestRequireOCITokenFromSecretsManager(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{
		"oci-docker.io-username": "docker-user",
		"oci-docker.io-token":    "docker-pass",
	})

	user, pass, err := RequireOCIToken(f, "docker.io")
	if err != nil {
		t.Fatalf("RequireOCIToken() error = %v", err)
	}
	if user != "docker-user" || pass != "docker-pass" {
		t.Errorf("RequireOCIToken() = (%q, %q), want (%q, %q)", user, pass, "docker-user", "docker-pass")
	}
}

func TestRequireOCITokenErrorsWhenNotATerminal(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	_, _, err := RequireOCIToken(f, "docker.io")
	if err == nil {
		t.Fatal("RequireOCIToken() = nil error, want an error when no credentials and not a TTY")
	}
	if !strings.Contains(err.Error(), "docker.io") {
		t.Errorf("error %q should mention the registry", err)
	}
}

func TestGetOCITokenNeverErrors(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	user, pass := GetOCIToken(f, "docker.io")
	if user != "" || pass != "" {
		t.Errorf("GetOCIToken() = (%q, %q), want empty strings when nothing is stored", user, pass)
	}
}

func TestTokenForRegistryGithub(t *testing.T) {
	t.Setenv("oci_token", "")
	t.Setenv("GITHUB_TOKEN", "")
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "gh-secret"})

	token, err := TokenForRegistry(f, "ghcr.io")
	if err != nil {
		t.Fatalf("TokenForRegistry() error = %v", err)
	}
	if token != "gh-secret" {
		t.Errorf("TokenForRegistry() = %q, want %q", token, "gh-secret")
	}
	if got := os.Getenv("oci_token"); got != "gh-secret" {
		t.Errorf("oci_token env = %q, want %q", got, "gh-secret")
	}
	if got := os.Getenv("GITHUB_TOKEN"); got != "gh-secret" {
		t.Errorf("GITHUB_TOKEN env = %q, want %q", got, "gh-secret")
	}
}

func TestTokenForRegistryEmpty(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	token, err := TokenForRegistry(f, "")
	if err != nil {
		t.Fatalf("TokenForRegistry() error = %v", err)
	}
	if token != "" {
		t.Errorf("TokenForRegistry() = %q, want empty string", token)
	}
}

func TestOptionalTokenForRegistryNeverFails(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	if got := OptionalTokenForRegistry(f, "docker.io"); got != "" {
		t.Errorf("OptionalTokenForRegistry() = %q, want empty string when nothing is stored", got)
	}
}

func TestOptionalTokenForRegistryEmptyRegistry(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, nil)

	if got := OptionalTokenForRegistry(f, ""); got != "" {
		t.Errorf("OptionalTokenForRegistry() = %q, want empty string", got)
	}
}

func TestOptionalTokenForRegistryFromSecretsManager(t *testing.T) {
	t.Setenv("oci_token", "")
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{
		"oci-docker.io-token": "docker-pass",
	})

	got := OptionalTokenForRegistry(f, "docker.io")
	if got != "docker-pass" {
		t.Errorf("OptionalTokenForRegistry() = %q, want %q", got, "docker-pass")
	}
	if env := os.Getenv("oci_token"); env != "docker-pass" {
		t.Errorf("oci_token env = %q, want %q", env, "docker-pass")
	}
}

func TestServiceFromArgsOCI(t *testing.T) {
	svc, rest, err := ServiceFromArgs([]string{"oci", "docker.io", "user", "pass"})
	if err != nil {
		t.Fatalf("ServiceFromArgs() error = %v", err)
	}
	if svc.ID != "oci:docker.io" {
		t.Errorf("ServiceFromArgs() service ID = %q, want %q", svc.ID, "oci:docker.io")
	}
	if len(rest) != 2 || rest[0] != "user" || rest[1] != "pass" {
		t.Errorf("ServiceFromArgs() rest = %v, want [user pass]", rest)
	}
}

func TestServiceFromArgsUnknown(t *testing.T) {
	_, _, err := ServiceFromArgs([]string{"nope"})
	if err == nil {
		t.Fatal("ServiceFromArgs() = nil error, want error for unknown service")
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("abcd"); got != "****" {
		t.Errorf("Redact(short) = %q, want %q", got, "****")
	}
	if got := Redact("abcdefgh"); got != "****efgh" {
		t.Errorf("Redact(long) = %q, want %q", got, "****efgh")
	}
}
