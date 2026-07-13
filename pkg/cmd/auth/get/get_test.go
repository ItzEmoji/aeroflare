package get

import (
	"errors"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

func TestGet_PrintsRawToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "secret-gh"})

	cmd := NewCmdGet(f)
	cmd.SetArgs([]string{"github"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if got := strings.TrimSpace(out.String()); got != "secret-gh" {
		t.Errorf("expected raw token %q, got %q", "secret-gh", got)
	}
}

func TestGet_MissingCredential_Errors(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	cmd := NewCmdGet(f)
	cmd.SetArgs([]string{"gitlab"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when credential is missing")
	}
}

func TestGet_FieldResolutionError_WrappsContextAndUnderlying(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	// Replace the secrets manager with one that returns an error on Get
	mockErr := errors.New("keychain access denied")
	mock := &errorReturningManager{err: mockErr}
	f.Secrets = func() cmdutil.SecretsManager { return mock }

	cmd := NewCmdGet(f)
	cmd.SetArgs([]string{"github", "token"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}

	// Verify error message includes both context and underlying cause
	if !strings.Contains(err.Error(), "no value found for GitHub token") {
		t.Errorf("error message missing context: %v", err)
	}
	if !errors.Is(err, mockErr) {
		t.Errorf("error does not wrap underlying error; errors.Is(err, mockErr) = false")
	}
}

// errorReturningManager is a test secrets manager that returns an error on Get.
type errorReturningManager struct {
	err error
}

func (m *errorReturningManager) Get(key string) (string, error) {
	return "", m.err
}

func (m *errorReturningManager) Set(key, value string) error {
	return nil
}

func (m *errorReturningManager) List() ([]string, error) {
	return nil, nil
}

func (m *errorReturningManager) Delete(key string) error {
	return nil
}
