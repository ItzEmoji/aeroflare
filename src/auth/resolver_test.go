package auth_test

import (
	"errors"
	"os"
	"testing"
	"aeroflare/src/auth"
)

func TestResolver_FlagPriority(t *testing.T) {
	os.Setenv("TEST_ENV_VAR", "env-value")
	defer os.Unsetenv("TEST_ENV_VAR")

	val, err := auth.NewResolver("test-secret").
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
	os.Setenv("TEST_ENV_VAR", "env-value")
	defer os.Unsetenv("TEST_ENV_VAR")
	
	val, err := auth.NewResolver("test-secret").
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
	_, err := auth.NewResolver("test-missing-secret").
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
	return "", errors.New("not found")
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
