package secrets_test

import (
	"errors"
	"os"
	"testing"
	"aeroflare/src/secrets"

	"github.com/zalando/go-keyring"
)

func TestFallbackManager(t *testing.T) {
	// Mock keyring to return an error, forcing the fallback path
	keyring.MockInitWithError(errors.New("simulated error"))

	// Use a dummy config dir for tests to avoid touching real keychains or config files
	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")

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
