package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

// fakeKeychain is an injectable keychain backend for exercising the manager's
// availability and fallback logic without a real OS keychain.
type fakeKeychain struct {
	mu          sync.Mutex
	data        map[string]string
	unreachable bool             // every op returns a generic connection error
	getErr      map[string]error // per-key error override for Get
	setErr      error            // returned by Set when the backend is reachable
	deleteErr   map[string]error // per-key error override for Delete
}

var errUnreachable = errors.New("dbus: connection refused")

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{data: map[string]string{}, getErr: map[string]error{}, deleteErr: map[string]error{}}
}

func (f *fakeKeychain) Get(service, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unreachable {
		return "", errUnreachable
	}
	if err := f.getErr[key]; err != nil {
		return "", err
	}
	if v, ok := f.data[key]; ok {
		return v, nil
	}
	return "", keyring.ErrNotFound
}

func (f *fakeKeychain) Set(service, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unreachable {
		return errUnreachable
	}
	if f.setErr != nil {
		return f.setErr
	}
	f.data[key] = value
	return nil
}

func (f *fakeKeychain) Delete(service, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unreachable {
		return errUnreachable
	}
	if err := f.deleteErr[key]; err != nil {
		return err
	}
	if _, ok := f.data[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.data, key)
	return nil
}

func withTempConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
}

// A reachable keychain is the authoritative backend: Set lands there and never
// touches the plaintext fallback file.
func TestSetUsesKeychainAndNotPlaintextWhenReachable(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	if err := m.Set("github-token", "ghp_secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if kc.data["github-token"] != "ghp_secret" {
		t.Errorf("token not stored in keychain: %v", kc.data)
	}
	if _, err := os.Stat(getFallbackFile()); !os.IsNotExist(err) {
		t.Errorf("plaintext fallback file was written despite a reachable keychain (err=%v)", err)
	}
}

// When the keychain is reachable but the write fails, the manager must NOT
// silently downgrade to writing the secret in plaintext; it surfaces the error.
func TestSetSurfacesErrorInsteadOfPlaintextDowngrade(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	kc.setErr = errors.New("keychain is locked")
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	err := m.Set("github-token", "ghp_secret")
	if err == nil {
		t.Fatal("Set on a reachable-but-failing keychain returned nil, want error")
	}
	if _, statErr := os.Stat(getFallbackFile()); !os.IsNotExist(statErr) {
		t.Errorf("secret was silently written to plaintext fallback (stat err=%v)", statErr)
	}
}

// The core masking fix: a reachable keychain that transiently fails a read must
// surface the error, not report the credential as absent (ErrNotFound).
func TestGetSurfacesTransientKeychainError(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	kc.getErr["github-token"] = errors.New("keychain is locked")
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	_, err := m.Get("github-token")
	if err == nil {
		t.Fatal("Get returned nil error for a locked keychain")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("Get masked a keychain failure as ErrNotFound: %v", err)
	}
}

// An unreachable keychain (headless CI) is the documented fallback case: writes
// go to the plaintext file and reads come back from it, with keychain errors
// staying quiet.
func TestUnreachableKeychainFallsBackToFile(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	kc.unreachable = true
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	if err := m.Set("github-token", "ghp_secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := m.Get("github-token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "ghp_secret" {
		t.Errorf("got %q, want ghp_secret", got)
	}
	if _, err := os.Stat(getFallbackFile()); err != nil {
		t.Errorf("expected plaintext fallback file to exist: %v", err)
	}
}

// A missing credential on a reachable keychain with no fallback file is a
// genuine ErrNotFound, distinguishable from a backend failure.
func TestGetMissingOnReachableKeychainIsErrNotFound(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	_, err := m.Get("never-stored")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get of absent key = %v, want ErrNotFound", err)
	}
}

// Delete honesty: a reachable keychain that fails to delete must surface the
// error rather than reporting success.
func TestDeleteSurfacesKeychainError(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	kc.data["github-token"] = "ghp_secret"
	kc.deleteErr["github-token"] = errors.New("keychain is locked")
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	if err := m.Delete("github-token"); err == nil {
		t.Fatal("Delete on a reachable-but-failing keychain returned nil, want error")
	}
}

// Deleting an absent key is not an error, and it still clears any fallback copy.
func TestDeleteAbsentKeyIsNotError(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	if err := m.Delete("never-stored"); err != nil {
		t.Errorf("Delete of absent key = %v, want nil", err)
	}
}

// A corrupt fallback file must not wedge writes: the manager preserves the
// corrupt file (as a backup) and still stores the new secret.
func TestCorruptFallbackFileDoesNotWedgeWrites(t *testing.T) {
	withTempConfig(t)
	dir := getConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(getFallbackFile(), []byte("{ this is not json"), 0600); err != nil {
		t.Fatal(err)
	}
	kc := newFakeKeychain()
	kc.unreachable = true
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	if err := m.Set("github-token", "ghp_secret"); err != nil {
		t.Fatalf("Set over a corrupt fallback file failed: %v", err)
	}
	got, err := m.Get("github-token")
	if err != nil || got != "ghp_secret" {
		t.Fatalf("Get after recovery = (%q, %v), want (ghp_secret, nil)", got, err)
	}
	// The corrupt contents should have been preserved as a backup, not lost.
	backups, _ := filepath.Glob(getFallbackFile() + ".corrupt*")
	if len(backups) == 0 {
		t.Errorf("expected the corrupt file to be backed up, found none")
	}
}

// Concurrent writes to the file backend must not clobber each other via a
// shared temp filename, and every key must survive in the index.
func TestConcurrentSetKeepsAllKeys(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	kc.unreachable = true
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Set("key"+string(rune('a'+i)), "v")
		}(i)
	}
	wg.Wait()

	keys, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != n {
		t.Errorf("got %d keys after %d concurrent writes, want %d: %v", len(keys), n, n, keys)
	}
}

// The index is stored in a stable, sorted order so repeated writes don't churn
// its value needlessly.
func TestIndexIsSorted(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	m := newManagerWithKeychain(kc, new(bytes.Buffer))

	for _, k := range []string{"c", "a", "b"} {
		if err := m.Set(k, "v"); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := m.Get("_keys_index")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(idx, ",")
	if !sort.StringsAreSorted(parts) {
		t.Errorf("index not sorted: %q", idx)
	}
}

// Status chatter must not go to stdout, which `auth get` reserves for the raw
// credential value; it goes to the injected status writer.
func TestSetStatusGoesToStatusWriterNotStdout(t *testing.T) {
	withTempConfig(t)
	kc := newFakeKeychain()
	status := new(bytes.Buffer)
	m := newManagerWithKeychain(kc, status)

	if err := m.Set("github-token", "ghp_secret"); err != nil {
		t.Fatal(err)
	}
	if status.Len() == 0 {
		t.Errorf("expected a status message on the status writer, got none")
	}
}
