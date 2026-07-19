package secrets_test

import (
	"errors"
	"github.com/itzemoji/aeroflare/internal/secrets"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestFallbackManager(t *testing.T) {
	// Mock keyring to return an error, forcing the fallback path
	keyring.MockInitWithError(errors.New("simulated error"))

	// Use a dummy config dir for tests to avoid touching real keychains or config files
	tmpDir := t.TempDir()
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Unsetenv("XDG_CONFIG_HOME") }()

	manager := secrets.NewManager()

	err := manager.Set("test-key", "test-value")
	if err != nil {
		t.Fatalf("Failed to set secret: %v", err)
	}

	val, err := manager.Get("test-key")
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	if val != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", val)
	}
}

// A missing fallback file means "this secret was never stored", not a failure.
// Callers distinguish the two by matching ErrNotFound, so leaking the raw
// os.ReadFile error made them report a spurious warning on a fresh machine.
func TestGetMissingFallbackFileReturnsErrNotFound(t *testing.T) {
	keyring.MockInitWithError(errors.New("simulated error"))

	tmpDir := t.TempDir()
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Unsetenv("XDG_CONFIG_HOME") }()

	manager := secrets.NewManager()

	_, err := manager.Get("never-stored")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("Get() on a machine with no secrets file = %v, want ErrNotFound", err)
	}
}
