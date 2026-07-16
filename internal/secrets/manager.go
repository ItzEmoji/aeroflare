// Package secrets stores and retrieves credentials (GitHub/GitLab/OCI
// tokens) using the OS keychain via go-keyring as the primary backend. If
// the keychain is unavailable (e.g. headless CI, no D-Bus secret service),
// it transparently falls back to a plain-text JSON file under the user's
// config directory, so aeroflare keeps working in restricted environments
// at the cost of weaker credential protection.
package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned by Manager.Get when the requested key does not
// exist in either the keychain or the fallback file.
var ErrNotFound = keyring.ErrNotFound

// Manager stores and retrieves named secrets. Implementations may back onto
// the OS keychain, a file, or (in tests) an in-memory map.
type Manager interface {
	Set(key, value string) error
	Get(key string) (string, error)
	List() ([]string, error)
	Delete(key string) error
}

// defaultManager is the production Manager: it prefers the OS keychain,
// identified by serviceName, and falls back to a JSON file when the
// keychain is unavailable.
type defaultManager struct {
	serviceName string
}

// NewManager returns the default Manager, scoped to aeroflare's own
// keychain service name so its secrets don't collide with other apps'.
func NewManager() Manager {
	return &defaultManager{serviceName: "aeroflare"}
}

// getConfigDir returns aeroflare's config directory: $XDG_CONFIG_HOME/aeroflare
// if set, otherwise ~/.config/aeroflare.
func getConfigDir() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil && homeDir != "" {
			configDir = filepath.Join(homeDir, ".config")
		}
	}
	return filepath.Join(configDir, "aeroflare")
}

// getFallbackFile returns the path to the plain-text JSON file used when
// the OS keychain is unavailable.
func getFallbackFile() string {
	return filepath.Join(getConfigDir(), "secrets.json")
}

// updateIndex maintains the "_keys_index" pseudo-entry: a comma-separated
// list of all keys ever Set, stored via the same Get/Set path as any other
// secret. The OS keychain has no "list all keys" API, so List() depends on
// this index to enumerate keychain-backed secrets (in addition to whatever
// it finds in the fallback JSON file, which can list its own keys
// directly). The index entry itself is excluded from indexing to avoid
// infinite recursion.
func (m *defaultManager) updateIndex(key string, remove bool) {
	if key == "_keys_index" {
		return
	}
	indexStr, _ := m.Get("_keys_index")
	keys := make(map[string]bool)
	if indexStr != "" {
		for _, k := range strings.Split(indexStr, ",") {
			if k != "" {
				keys[k] = true
			}
		}
	}

	changed := false
	if remove {
		if keys[key] {
			delete(keys, key)
			changed = true
		}
	} else {
		if !keys[key] {
			keys[key] = true
			changed = true
		}
	}

	if changed {
		var newKeys []string
		for k := range keys {
			newKeys = append(newKeys, k)
		}
		_ = m.Set("_keys_index", strings.Join(newKeys, ","))
	}
}

// Set stores value under key in the OS keychain. If the keychain is
// unavailable, it falls back to writing (or updating) the plain-text JSON
// file, using a write-to-temp-then-rename to avoid corrupting the file on a
// crash mid-write. Either way, the key is recorded in the "_keys_index" so
// List can find it later.
func (m *defaultManager) Set(key, value string) error {
	err := keyring.Set(m.serviceName, key, value)
	if err == nil {
		if key != "_keys_index" {
			fmt.Printf("🔒 Secret '%s' securely written to keychain.\n", key)
		}
		m.updateIndex(key, false)
		return nil
	}

	if key != "_keys_index" {
		fmt.Printf("⚠️ Warning: Keychain not available. Secret '%s' written in plain text to fallback file.\n", key)
	}

	// Fall back to the plain-text JSON file.
	dir := getConfigDir()
	_ = os.MkdirAll(dir, 0755)

	file := getFallbackFile()
	stored := make(map[string]string)

	data, err := os.ReadFile(file)
	if err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &stored); err != nil {
				return err
			}
		}
	}

	stored[key] = value

	out, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := file + ".tmp"
	if err := os.WriteFile(tmpFile, out, 0600); err != nil {
		return err
	}
	err = os.Rename(tmpFile, file)
	if err == nil {
		m.updateIndex(key, false)
	}
	return err
}

// Get retrieves the value for key from the OS keychain, falling back to the
// plain-text JSON file if the keychain lookup fails (e.g. because the
// keychain is unavailable, or the key was written there by Set's fallback
// path). Returns ErrNotFound if key is present in neither.
func (m *defaultManager) Get(key string) (string, error) {
	val, err := keyring.Get(m.serviceName, key)
	if err == nil {
		return val, nil
	}

	// Fall back to the plain-text JSON file. Its absence means the secret was
	// never stored, which callers must be able to tell apart from a real read
	// failure by matching ErrNotFound.
	file := getFallbackFile()
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	stored := make(map[string]string)
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", err
	}

	if v, ok := stored[key]; ok {
		return v, nil
	}

	return "", keyring.ErrNotFound
}

// List returns all known secret keys, merging the "_keys_index" (which
// tracks keychain-backed keys, since the keychain has no native listing
// API) with whatever keys are present in the fallback JSON file. The
// internal "_keys_index" entry itself is never included in the result.
func (m *defaultManager) List() ([]string, error) {
	keySet := make(map[string]bool)

	indexStr, _ := m.Get("_keys_index")
	if indexStr != "" {
		for _, k := range strings.Split(indexStr, ",") {
			if k != "" {
				keySet[k] = true
			}
		}
	}

	// Merge keys from the fallback JSON file.
	file := getFallbackFile()
	data, err := os.ReadFile(file)
	if err == nil {
		stored := make(map[string]string)
		if json.Unmarshal(data, &stored) == nil {
			for k := range stored {
				keySet[k] = true
			}
		}
	}

	var keys []string
	for k := range keySet {
		if k != "_keys_index" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Delete removes key from both the OS keychain and the fallback JSON file
// (whichever holds it) and from the "_keys_index". Keychain errors are
// ignored: the key may simply not exist there (e.g. it was only ever
// written to the fallback file), which is not treated as a failure.
func (m *defaultManager) Delete(key string) error {
	_ = keyring.Delete(m.serviceName, key)
	// If keyring fails for another reason, we might still want to try fallback.
	// But usually it's fine.

	// Also try removing from the fallback JSON file.
	file := getFallbackFile()
	data, errFallback := os.ReadFile(file)
	if errFallback == nil {
		stored := make(map[string]string)
		if json.Unmarshal(data, &stored) == nil {
			if _, ok := stored[key]; ok {
				delete(stored, key)
				out, _ := json.MarshalIndent(stored, "", "  ")
				tmpFile := file + ".tmp"
				_ = os.WriteFile(tmpFile, out, 0600)
				_ = os.Rename(tmpFile, file)
			}
		}
	}

	m.updateIndex(key, true)
	return nil
}
