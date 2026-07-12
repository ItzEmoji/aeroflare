package get

import (
	"strings"
	"testing"

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
