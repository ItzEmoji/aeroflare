package login

import (
	"errors"
	"testing"

	"github.com/itzemoji/aeroflare/internal/secrets/secretstest"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

func TestLoginSavesProvidedTokens(t *testing.T) {
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{})
	f.Overrides.GithubToken = "test-gh"
	f.Overrides.GitlabToken = "test-gl"
	f.Overrides.CfToken = "test-cf"
	f.Overrides.CfAccountID = "test-id"

	cmd := NewCmdLogin(f)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	mgr := f.Secrets()
	for key, want := range map[string]string{
		"github-token":  "test-gh",
		"gitlab-token":  "test-gl",
		"cf-token":      "test-cf",
		"cf-account-id": "test-id",
	} {
		got, err := mgr.Get(key)
		if err != nil || got != want {
			t.Errorf("secret %s = %q, %v; want %q, nil", key, got, err, want)
		}
	}

	if out.Len() == 0 {
		t.Error("expected login to print a confirmation, got no output")
	}
}

func TestLogin_PropagatesSecretsManagerError(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})
	mock := secretstest.NewMockManager(map[string]string{})
	mock.Err = errors.New("mock error")
	f.Secrets = func() cmdutil.SecretsManager { return mock }
	f.Overrides.GithubToken = "fail"

	cmd := NewCmdLogin(f)
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected an error, got nil")
	}
}
