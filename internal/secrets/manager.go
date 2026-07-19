// Package secrets stores and retrieves credentials (GitHub/GitLab/OCI
// tokens) using the OS keychain via go-keyring as the primary backend. If
// the keychain is unavailable (e.g. headless CI, no D-Bus secret service),
// it transparently falls back to a plain-text JSON file under the user's
// config directory, so aeroflare keeps working in restricted environments
// at the cost of weaker credential protection.
//
// The two failure modes a keychain can present — "unreachable" (no D-Bus,
// headless CI) and "reachable but this operation failed" (locked, permission
// denied) — look identical at the go-keyring API level on Linux: both surface
// as a generic error, while only a genuinely absent key returns ErrNotFound.
// The manager tells them apart with a one-shot availability probe: if the
// keychain answers a probe read at all (a value or ErrNotFound), it is treated
// as the authoritative backend and its errors are surfaced rather than masked
// or silently downgraded to plaintext. If the probe itself fails, the manager
// is in fallback mode and keychain errors stay quiet.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// ErrNotFound is returned by Manager.Get when the requested key does not
// exist in either the keychain or the fallback file.
var ErrNotFound = keyring.ErrNotFound

// indexKey is the pseudo-entry holding the comma-separated list of all keys
// ever Set, used by List because the keychain has no native enumeration API.
const indexKey = "_keys_index"

// probeKey is the sentinel key read once to decide whether the keychain is
// reachable. It is never written.
const probeKey = "_aeroflare_probe"

// Manager stores and retrieves named secrets. Implementations may back onto
// the OS keychain, a file, or (in tests) an in-memory map.
type Manager interface {
	Set(key, value string) error
	Get(key string) (string, error)
	List() ([]string, error)
	Delete(key string) error
}

// keychain abstracts the OS keychain operations the manager needs, so the
// availability and fallback logic can be exercised without a real keychain.
type keychain interface {
	Set(service, key, value string) error
	Get(service, key string) (string, error)
	Delete(service, key string) error
}

// goKeyring is the production keychain, backed by go-keyring.
type goKeyring struct{}

func (goKeyring) Set(service, key, value string) error { return keyring.Set(service, key, value) }
func (goKeyring) Get(service, key string) (string, error) {
	return keyring.Get(service, key)
}
func (goKeyring) Delete(service, key string) error { return keyring.Delete(service, key) }

// defaultManager is the production Manager: it prefers the OS keychain,
// identified by serviceName, and falls back to a JSON file when the keychain
// is unavailable. A single mutex serializes the read-modify-write of the key
// index and the fallback file so concurrent callers can't clobber each other.
type defaultManager struct {
	serviceName string
	kc          keychain
	statusOut   io.Writer

	mu sync.Mutex

	probeOnce sync.Once
	available bool
}

// NewManager returns the default Manager, scoped to aeroflare's own keychain
// service name so its secrets don't collide with other apps'. Status messages
// are written to stderr, keeping stdout clean for `auth get`'s piped output.
func NewManager() Manager {
	return newManagerWithKeychain(goKeyring{}, os.Stderr)
}

// newManagerWithKeychain builds a defaultManager over an injected keychain and
// status writer, the seam tests use to drive the reachable/unreachable paths.
func newManagerWithKeychain(kc keychain, statusOut io.Writer) *defaultManager {
	if statusOut == nil {
		statusOut = io.Discard
	}
	return &defaultManager{serviceName: "aeroflare", kc: kc, statusOut: statusOut}
}

// keychainAvailable reports whether the OS keychain is reachable, probing once
// and caching the result. A probe that returns a value or ErrNotFound means the
// backend answered and is authoritative; any other error means it is
// unreachable and the fallback file is in charge.
func (m *defaultManager) keychainAvailable() bool {
	m.probeOnce.Do(func() {
		_, err := m.kc.Get(m.serviceName, probeKey)
		m.available = err == nil || errors.Is(err, keyring.ErrNotFound)
	})
	return m.available
}

func (m *defaultManager) status(format string, args ...any) {
	_, _ = fmt.Fprintf(m.statusOut, format, args...)
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

// getFallbackFile returns the path to the plain-text JSON file used when the OS
// keychain is unavailable.
func getFallbackFile() string {
	return filepath.Join(getConfigDir(), "secrets.json")
}

// Set stores value under key. When the keychain is reachable it is the sole
// backend: a failure is returned rather than silently written to plaintext.
// When the keychain is unreachable the value is written to the fallback JSON
// file. Either way the key is recorded in the index so List can find it.
func (m *defaultManager) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.keychainAvailable() {
		if err := m.kc.Set(m.serviceName, key, value); err != nil {
			return fmt.Errorf("keychain write failed for %q: %w", key, err)
		}
		if key != indexKey {
			m.status("🔒 Secret '%s' securely written to keychain.\n", key)
		}
		m.updateIndexLocked(key, false)
		return nil
	}

	if key != indexKey {
		m.status("⚠️ Warning: Keychain not available. Secret '%s' written in plain text to fallback file.\n", key)
	}
	if err := m.writeFallback(key, value); err != nil {
		return err
	}
	m.updateIndexLocked(key, false)
	return nil
}

// Get retrieves the value for key. On a reachable keychain, a stored value is
// returned and any error other than ErrNotFound is surfaced (never masked as
// "not found"); a genuinely absent key falls through to the fallback file. On
// an unreachable keychain, only the fallback file is consulted.
func (m *defaultManager) Get(key string) (string, error) {
	if m.keychainAvailable() {
		val, err := m.kc.Get(m.serviceName, key)
		if err == nil {
			return val, nil
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("keychain read failed for %q: %w", key, err)
		}
		// Reachable but absent: fall through to the file, which may hold a
		// value written on an earlier run when the keychain was unavailable.
	}

	stored, err := m.readFallback()
	if err != nil {
		return "", err
	}
	if v, ok := stored[key]; ok {
		return v, nil
	}
	return "", ErrNotFound
}

// List returns all known secret keys, merging the index (which tracks
// keychain-backed keys) with whatever keys are present in the fallback JSON
// file. The internal index entry itself is never included.
func (m *defaultManager) List() ([]string, error) {
	keySet := make(map[string]bool)

	indexStr, _ := m.Get(indexKey)
	for _, k := range strings.Split(indexStr, ",") {
		if k != "" {
			keySet[k] = true
		}
	}

	if stored, err := m.readFallback(); err == nil {
		for k := range stored {
			keySet[k] = true
		}
	}

	var keys []string
	for k := range keySet {
		if k != indexKey {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Delete removes key from whichever backend holds it and from the index. On a
// reachable keychain a delete error other than ErrNotFound is surfaced, so a
// credential the caller believes is gone cannot silently persist. The fallback
// file is always cleaned up as well.
func (m *defaultManager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.keychainAvailable() {
		if err := m.kc.Delete(m.serviceName, key); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("keychain delete failed for %q: %w", key, err)
		}
	}

	stored, err := m.readFallback()
	if err == nil {
		if _, ok := stored[key]; ok {
			delete(stored, key)
			if err := m.writeFallbackMap(stored); err != nil {
				return err
			}
		}
	}

	m.updateIndexLocked(key, true)
	return nil
}

// updateIndexLocked maintains the index pseudo-entry. The caller must hold m.mu.
// The index is never itself indexed, avoiding infinite recursion. Keys are
// stored sorted so repeated writes produce a stable value.
func (m *defaultManager) updateIndexLocked(key string, remove bool) {
	if key == indexKey {
		return
	}

	indexStr := m.getRaw(indexKey)
	keys := make(map[string]bool)
	for _, k := range strings.Split(indexStr, ",") {
		if k != "" {
			keys[k] = true
		}
	}

	changed := false
	if remove {
		if keys[key] {
			delete(keys, key)
			changed = true
		}
	} else if !keys[key] {
		keys[key] = true
		changed = true
	}
	if !changed {
		return
	}

	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	m.setRaw(indexKey, strings.Join(sorted, ","))
}

// getRaw reads a key from the active backend without acquiring m.mu (callers
// already hold it) and without recursing through the index machinery.
func (m *defaultManager) getRaw(key string) string {
	if m.keychainAvailable() {
		if v, err := m.kc.Get(m.serviceName, key); err == nil {
			return v
		} else if !errors.Is(err, keyring.ErrNotFound) {
			return ""
		}
	}
	stored, err := m.readFallback()
	if err != nil {
		return ""
	}
	return stored[key]
}

// setRaw writes a key to the active backend without touching the index.
func (m *defaultManager) setRaw(key, value string) {
	if m.keychainAvailable() {
		_ = m.kc.Set(m.serviceName, key, value)
		return
	}
	_ = m.writeFallback(key, value)
}

// writeFallback merges key=value into the fallback file and persists it.
func (m *defaultManager) writeFallback(key, value string) error {
	stored, err := m.readFallback()
	if err != nil {
		return err
	}
	stored[key] = value
	return m.writeFallbackMap(stored)
}

// readFallback loads the fallback JSON file. A missing file is an empty map. A
// corrupt file is preserved as a timestamped backup and treated as empty, so a
// single bad file cannot wedge every future read and write.
func (m *defaultManager) readFallback() (map[string]string, error) {
	stored := make(map[string]string)
	file := getFallbackFile()
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return stored, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return stored, nil
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		backup := fmt.Sprintf("%s.corrupt.%d", file, time.Now().UnixNano())
		if renameErr := os.Rename(file, backup); renameErr == nil {
			m.status("⚠️ Warning: fallback file %s was corrupt; preserved as %s.\n", file, backup)
		}
		return make(map[string]string), nil
	}
	return stored, nil
}

// writeFallbackMap persists stored to the fallback file via a uniquely named
// temp file and an atomic rename, so concurrent writers cannot clobber a shared
// temp path or corrupt the file on a crash mid-write.
func (m *defaultManager) writeFallbackMap(stored map[string]string) error {
	dir := getConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "secrets-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, getFallbackFile())
}
