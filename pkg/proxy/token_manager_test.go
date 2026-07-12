package proxy

import (
	"context"
	"testing"
)

// The proxy is a library: it must never reach into the ambient environment for
// credentials. A caller that sets no override gets no override, even when
// oci_token / NIXCACHE_TOKEN are set in the process environment.
func TestNewTokenManager_IgnoresEnvironment(t *testing.T) {
	t.Setenv("oci_token", "env-bearer-should-be-ignored")
	t.Setenv("NIXCACHE_TOKEN", "env-bearer-should-be-ignored")

	tm := NewTokenManager("ghcr.io", "test-repo", "")

	if tm.overrideToken != "" {
		t.Fatalf("overrideToken = %q, want empty: the environment must not be consulted", tm.overrideToken)
	}
}

func TestSetOverrideToken_UsedVerbatim(t *testing.T) {
	tm := NewTokenManager("ghcr.io", "test-repo", "")
	tm.SetOverrideToken("a-real-bearer")

	got, err := tm.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got != "a-real-bearer" {
		t.Fatalf("GetToken() = %q, want the override used verbatim", got)
	}
}

// A raw PAT is not a valid OCI bearer token. Someone who pastes one into
// oci_token must fall through to normal token exchange rather than have an
// invalid Authorization header sent on their behalf.
func TestSetOverrideToken_RejectsRawPATs(t *testing.T) {
	pats := []string{
		"ghp_aaaaaaaaaaaaaaaa",
		"github_pat_aaaaaaaaaaaa",
		"glpat-aaaaaaaaaaaaaaaa",
		"gho_aaaaaaaaaaaaaaaa",
		"ghu_aaaaaaaaaaaaaaaa",
		"ghs_aaaaaaaaaaaaaaaa",
	}
	for _, pat := range pats {
		tm := NewTokenManager("ghcr.io", "test-repo", "")
		tm.SetOverrideToken(pat)

		if tm.overrideToken != "" {
			t.Errorf("SetOverrideToken(%q): overrideToken = %q, want empty (a raw PAT is not a bearer token)", pat, tm.overrideToken)
		}
	}
}
