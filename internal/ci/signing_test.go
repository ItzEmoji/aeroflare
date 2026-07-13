package ci

import (
	"os"
	"testing"
)

func TestResolveSigningKey_Empty(t *testing.T) {
	path, cleanup, err := ResolveSigningKey("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestResolveSigningKey_EnvMaterial(t *testing.T) {
	t.Setenv("MY_SIGN_KEY", "secret-key-material")
	path, cleanup, err := ResolveSigningKey("MY_SIGN_KEY")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-key-material" {
		t.Errorf("content = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 600", info.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file should be removed after cleanup")
	}
}

func TestResolveSigningKey_Path(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "key-*")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	path, cleanup, err := ResolveSigningKey(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if path != f.Name() {
		t.Errorf("path = %q, want %q", path, f.Name())
	}
}

func TestResolveSigningKey_Invalid(t *testing.T) {
	_, _, err := ResolveSigningKey("/nonexistent/not-an-env-var")
	if err == nil {
		t.Fatal("expected error")
	}
}
