package remove

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/cmdutil/cmdutiltest"
)

func TestRemove_DeletesServiceFields(t *testing.T) {
	f, _, _ := cmdutiltest.NewTestFactory(t, map[string]string{"cf-token": "a", "cf-user-id": "b"})

	cmd := NewCmdRemove(f)
	cmd.SetArgs([]string{"cloudflare"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want nil", err)
	}

	mgr := f.Secrets()
	if _, err := mgr.Get("cf-token"); err == nil {
		t.Errorf("cf-token should have been removed")
	}
	if _, err := mgr.Get("cf-user-id"); err == nil {
		t.Errorf("cf-user-id should have been removed")
	}
}
