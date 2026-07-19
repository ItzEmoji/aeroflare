package status

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

// Mirrors statusEntryJSON in status.go. Kept separate on purpose: if the
// emitted shape changes, this test should fail rather than silently follow.
type entry struct {
	Service string `json:"service"`
	ID      string `json:"id"`
	Fields  []struct {
		Name   string `json:"name"`
		Set    bool   `json:"set"`
		Secret bool   `json:"secret"`
	} `json:"fields"`
}

func TestStatusJSONReportsStoredCredentials(t *testing.T) {
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "ghp_xxx"})

	cmd := NewCmdStatus(f)
	cmd.SetArgs([]string{"--json", "--no-verify"}) // --no-verify: no network call
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	var entries []entry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot: %s", err, out.String())
	}

	var found bool
	for _, e := range entries {
		if e.Service != "GitHub" {
			continue
		}
		for _, fld := range e.Fields {
			if fld.Name == "token" {
				found = true
				if !fld.Set {
					t.Error("github token field reports Set: false, want true")
				}
				if !fld.Secret {
					t.Error("github token field reports Secret: false, want true")
				}
			}
		}
	}
	if !found {
		t.Errorf("no github token field in output: %s", out.String())
	}
}

func TestStatusJSON_IncludesGithubAndCloudflare(t *testing.T) {
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{
		"github-token": "gh",
		"cf-token":     "cf",
	})

	cmd := NewCmdStatus(f)
	cmd.SetArgs([]string{"--json", "--no-verify"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	var entries []entry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatalf("status --json did not emit valid JSON: %v\n%s", err, out.String())
	}
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	if !ids["github"] || !ids["cloudflare"] {
		t.Errorf("expected github and cloudflare in status, got %v", ids)
	}
}

func TestStatusTable_Columns(t *testing.T) {
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{
		"github-token":  "ghtokenvalue",
		"cf-token":      "cfval",
		"cf-account-id": "acct-1",
	})

	cmd := NewCmdStatus(f)
	cmd.SetArgs([]string{"--no-verify"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	got := out.String()
	for _, want := range []string{"Service", "ID", "Field", "github", "cloudflare", "token", "account_id", "acct-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected status table to contain %q, output:\n%s", want, got)
		}
	}
}

func TestStatusTable_RedactsSecrets(t *testing.T) {
	f, out, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "supersecretvalue"})

	cmd := NewCmdStatus(f)
	cmd.SetArgs([]string{"--no-verify"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	if strings.Contains(out.String(), "supersecretvalue") {
		t.Errorf("status leaked a secret value:\n%s", out.String())
	}
}

func TestStatusFlags_DoNotLeakBetweenExecutions(t *testing.T) {
	// A regression test for the win the refactor buys: flags live on a
	// per-command Options, so two NewCmdStatus(f) calls in the same process
	// don't share state the way the old package-level authStatusJSON did.
	f, out1, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "gh"})
	cmd1 := NewCmdStatus(f)
	cmd1.SetArgs([]string{"--json", "--no-verify"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if !json.Valid(out1.Bytes()) {
		t.Fatalf("expected valid JSON from first command, got: %s", out1.String())
	}

	f2, out2, _ := cmdutiltest.NewTestFactory(t, map[string]string{"github-token": "gh"})
	cmd2 := NewCmdStatus(f2)
	cmd2.SetArgs([]string{"--no-verify"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if json.Valid(out2.Bytes()) {
		t.Errorf("expected non-JSON table output from second command, but --json leaked in: %s", out2.String())
	}
}
