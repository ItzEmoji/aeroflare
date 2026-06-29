package auth_test

import (
	"testing"
	"aeroflare/src/auth"
	"aeroflare/src/secrets"
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
	return "", secrets.ErrNotFound
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

func TestResolveRegistryToken_GenericOCI(t *testing.T) {
	t.Setenv("oci_token", "test-oci-token")

	token, err := auth.ResolveRegistryToken("docker.io")
	if err != nil || token != "test-oci-token" {
		t.Errorf("expected test-oci-token for docker.io, got %s, err: %v", token, err)
	}
}
