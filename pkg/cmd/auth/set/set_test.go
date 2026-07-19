package set

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

func TestSet_SavesSingleFieldService(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	cmd := NewCmdSet(f)
	cmd.SetArgs([]string{"github", "gh-tok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	got, err := f.Secrets().Get("github-token")
	if err != nil || got != "gh-tok" {
		t.Errorf("github-token = %q, %v; want %q, nil", got, err, "gh-tok")
	}
}

func TestSet_UnknownService(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	cmd := NewCmdSet(f)
	cmd.SetArgs([]string{"notaservice", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected an error for an unknown service")
	}
}

func TestSet_MultiFieldAndOCI(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	cmd := NewCmdSet(f)
	cmd.SetArgs([]string{"cloudflare", "cf-tok", "acct-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	mgr := f.Secrets()
	if v, _ := mgr.Get("cf-token"); v != "cf-tok" {
		t.Errorf("cf-token = %q, want %q", v, "cf-tok")
	}
	if v, _ := mgr.Get("cf-account-id"); v != "acct-1" {
		t.Errorf("cf-account-id = %q, want %q", v, "acct-1")
	}

	cmd2 := NewCmdSet(f)
	cmd2.SetArgs([]string{"oci", "docker.io", "bob", "dckr-tok"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}
	if v, _ := mgr.Get("oci-docker.io-username"); v != "bob" {
		t.Errorf("oci-docker.io-username = %q, want %q", v, "bob")
	}
	if v, _ := mgr.Get("oci-docker.io-token"); v != "dckr-tok" {
		t.Errorf("oci-docker.io-token = %q, want %q", v, "dckr-tok")
	}
}

func TestSet_NoValuesNonInteractive_Errors(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{})

	cmd := NewCmdSet(f)
	cmd.SetArgs([]string{"github"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected an error when no values given and not interactive")
	}
}
