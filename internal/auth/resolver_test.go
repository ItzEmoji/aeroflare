package auth_test

import (
	"errors"
	"fmt"
	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/internal/secrets"
	"testing"
)

func TestResolver_FlagPriority(t *testing.T) {
	t.Setenv("TEST_ENV_VAR", "env-value")

	mock := &mockSecretsManager{
		data: map[string]string{
			"test-secret": "secret-value",
		},
	}

	val, err := auth.NewResolver("test-secret").
		WithSecretsManager(mock).
		WithFlag("flag-value").
		WithEnv("TEST_ENV_VAR").
		Resolve()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != "flag-value" {
		t.Errorf("expected flag-value, got %s", val)
	}
}

func TestResolver_EnvPriority(t *testing.T) {
	t.Setenv("TEST_ENV_VAR", "env-value")

	mock := &mockSecretsManager{
		data: map[string]string{
			"test-secret": "secret-value",
		},
	}

	val, err := auth.NewResolver("test-secret").
		WithSecretsManager(mock).
		WithFlag("").
		WithEnv("TEST_ENV_VAR").
		Resolve()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != "env-value" {
		t.Errorf("expected env-value, got %s", val)
	}
}

func TestResolver_NotFound(t *testing.T) {
	mock := &mockSecretsManager{
		data: map[string]string{},
	}

	_, err := auth.NewResolver("test-missing-secret").
		WithSecretsManager(mock).
		WithFlag("").
		WithEnv("NONEXISTENT_VAR").
		Resolve()

	if err != auth.ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

type mockSecretsManager struct {
	data map[string]string
	// getErr, when set, is returned by Get for a key that isn't in data,
	// standing in for a manager that wraps ErrNotFound (or fails outright).
	getErr error
}

func (m *mockSecretsManager) Set(key, value string) error {
	if m.data == nil {
		m.data = make(map[string]string)
	}
	m.data[key] = value
	return nil
}

func (m *mockSecretsManager) Get(key string) (string, error) {
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	if m.getErr != nil {
		return "", m.getErr
	}
	return "", secrets.ErrNotFound
}

// The secrets layer now wraps some errors, so a "not found" may arrive wrapped.
// Resolve must still recognize it as not-found (via errors.Is) and return
// ErrTokenNotFound rather than leaking the wrapped error to callers.
func TestResolver_WrappedNotFoundTreatedAsNotFound(t *testing.T) {
	mock := &mockSecretsManager{
		data:   map[string]string{},
		getErr: fmt.Errorf("keychain read failed for %q: %w", "test-secret", secrets.ErrNotFound),
	}

	_, err := auth.NewResolver("test-secret").
		WithSecretsManager(mock).
		Resolve()

	if !errors.Is(err, auth.ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound for a wrapped not-found, got %v", err)
	}
}

// A genuine (non-not-found) manager error must be surfaced, not turned into
// ErrTokenNotFound, so a locked keychain can't masquerade as a missing token.
func TestResolver_SurfacesNonNotFoundError(t *testing.T) {
	sentinel := errors.New("keychain is locked")
	mock := &mockSecretsManager{data: map[string]string{}, getErr: sentinel}

	_, err := auth.NewResolver("test-secret").
		WithSecretsManager(mock).
		Resolve()

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the underlying keychain error, got %v", err)
	}
}

func (m *mockSecretsManager) List() ([]string, error) {
	var keys []string
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *mockSecretsManager) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func TestResolver_SecretsManagerSuccess(t *testing.T) {
	mock := &mockSecretsManager{
		data: map[string]string{
			"test-secret": "secret-value",
		},
	}

	val, err := auth.NewResolver("test-secret").
		WithSecretsManager(mock).
		Resolve()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != "secret-value" {
		t.Errorf("expected secret-value, got %s", val)
	}
}

func TestResolveGithubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-gh-token")

	token, err := auth.ResolveGithubToken()
	if err != nil || token != "test-gh-token" {
		t.Errorf("expected test-gh-token, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-gh-token")

	token, err := auth.ResolveRegistryToken("ghcr.io")
	if err != nil || token != "test-gh-token" {
		t.Errorf("expected test-gh-token for ghcr.io, got %s, err: %v", token, err)
	}
}

func TestResolveGitlabToken(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-gl-token")

	token, err := auth.ResolveGitlabToken()
	if err != nil || token != "test-gl-token" {
		t.Errorf("expected test-gl-token, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken_Gitlab(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "test-gl-token")

	token, err := auth.ResolveRegistryToken("registry.gitlab.com")
	if err != nil || token != "test-gl-token" {
		t.Errorf("expected test-gl-token for registry.gitlab.com, got %s, err: %v", token, err)
	}
}

func TestResolveGithubToken_GHToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "test-gh-token-short")

	token, err := auth.ResolveGithubToken()
	if err != nil || token != "test-gh-token-short" {
		t.Errorf("expected test-gh-token-short, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken_GenericOCI(t *testing.T) {
	t.Setenv("oci_token", "")
	mock := &mockSecretsManager{
		data: map[string]string{
			"oci-docker.io-token": "test-oci-secret-token",
		},
	}

	token, err := auth.ResolveRegistryToken("docker.io", mock)
	if err != nil || token != "test-oci-secret-token" {
		t.Errorf("expected test-oci-secret-token for docker.io, got %s, err: %v", token, err)
	}
}

func TestResolveGithubToken_WithManager(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	mock := &mockSecretsManager{
		data: map[string]string{
			"github-token": "mock-gh-token",
		},
	}
	token, err := auth.ResolveGithubToken(mock)
	if err != nil || token != "mock-gh-token" {
		t.Errorf("expected mock-gh-token, got %s, err: %v", token, err)
	}
}

func TestResolveGitlabToken_WithManager(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	mock := &mockSecretsManager{
		data: map[string]string{
			"gitlab-token": "mock-gl-token",
		},
	}
	token, err := auth.ResolveGitlabToken(mock)
	if err != nil || token != "mock-gl-token" {
		t.Errorf("expected mock-gl-token, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken_OCIEnvVar(t *testing.T) {
	t.Setenv("oci_token", "test-val")
	token, err := auth.ResolveRegistryToken("docker.io")
	if err != nil || token != "test-val" {
		t.Errorf("expected test-val for docker.io via oci_token env var, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken_GithubWithManager(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	mock := &mockSecretsManager{
		data: map[string]string{
			"github-token": "mock-ghcr-token",
		},
	}
	token, err := auth.ResolveRegistryToken("ghcr.io", mock)
	if err != nil || token != "mock-ghcr-token" {
		t.Errorf("expected mock-ghcr-token for ghcr.io, got %s, err: %v", token, err)
	}
}

func TestResolveRegistryToken_GitlabWithManager(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	mock := &mockSecretsManager{
		data: map[string]string{
			"gitlab-token": "mock-gitlab-registry-token",
		},
	}
	token, err := auth.ResolveRegistryToken("registry.gitlab.com", mock)
	if err != nil || token != "mock-gitlab-registry-token" {
		t.Errorf("expected mock-gitlab-registry-token for registry.gitlab.com, got %s, err: %v", token, err)
	}
}
