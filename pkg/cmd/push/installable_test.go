package push

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// canonStore returns a canonicalized temp dir usable as a fake Nix store, so
// symlink resolution (which canonicalizes) can be compared by prefix.
func canonStore(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(tempdir) = %v", err)
	}
	return dir
}

func TestResolveInstallables_StorePathPassthrough(t *testing.T) {
	called := false
	r := &installableResolver{
		storeDir: "/nix/store",
		build: func(string) ([]string, error) {
			called = true
			return nil, nil
		},
	}

	got, err := r.resolve([]string{"/nix/store/abc-foo"})
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	if want := []string{"/nix/store/abc-foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("resolve() = %v, want %v", got, want)
	}
	if called {
		t.Error("build was called for an explicit store path")
	}
}

func TestResolveInstallables_ResultSymlink(t *testing.T) {
	store := canonStore(t)
	target := filepath.Join(store, "abc123-hello")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "result")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	called := false
	r := &installableResolver{
		storeDir: store,
		build: func(string) ([]string, error) {
			called = true
			return nil, nil
		},
	}

	got, err := r.resolve([]string{link})
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	if want := []string{target}; !reflect.DeepEqual(got, want) {
		t.Errorf("resolve() = %v, want %v", got, want)
	}
	if called {
		t.Error("build was called for an already-built result symlink")
	}
}

func TestResolveInstallables_SymlinkIntoSubpathTrimmedToStoreRoot(t *testing.T) {
	store := canonStore(t)
	root := filepath.Join(store, "abc123-hello")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "result-bin")
	if err := os.Symlink(filepath.Join(root, "bin"), link); err != nil {
		t.Fatal(err)
	}

	r := &installableResolver{
		storeDir: store,
		build:    func(string) ([]string, error) { return nil, nil },
	}

	got, err := r.resolve([]string{link})
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	if want := []string{root}; !reflect.DeepEqual(got, want) {
		t.Errorf("resolve() = %v, want %v (should trim to store root)", got, want)
	}
}

func TestResolveInstallables_BuildDispatch(t *testing.T) {
	var builtArg string
	r := &installableResolver{
		storeDir: "/nix/store",
		build: func(arg string) ([]string, error) {
			builtArg = arg
			return []string{"/nix/store/xxx-hello", "/nix/store/yyy-hello-man"}, nil
		},
	}

	got, err := r.resolve([]string{"nixpkgs#hello"})
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	if builtArg != "nixpkgs#hello" {
		t.Errorf("build called with %q, want nixpkgs#hello", builtArg)
	}
	want := []string{"/nix/store/xxx-hello", "/nix/store/yyy-hello-man"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolve() = %v, want %v", got, want)
	}
}

func TestResolveInstallables_MixedArgsPreserveOrder(t *testing.T) {
	store := canonStore(t)
	target := filepath.Join(store, "abc123-hello")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "result")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(store, "def456-foo")

	r := &installableResolver{
		storeDir: store,
		build: func(arg string) ([]string, error) {
			return []string{"/built/" + arg}, nil
		},
	}

	got, err := r.resolve([]string{explicit, link, "nixpkgs#hello"})
	if err != nil {
		t.Fatalf("resolve() = %v", err)
	}
	want := []string{explicit, target, "/built/nixpkgs#hello"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolve() = %v, want %v", got, want)
	}
}

func TestResolveInstallables_BuildErrorPropagates(t *testing.T) {
	sentinel := errors.New("nix build failed")
	r := &installableResolver{
		storeDir: "/nix/store",
		build:    func(string) ([]string, error) { return nil, sentinel },
	}

	_, err := r.resolve([]string{"nixpkgs#nope"})
	if !errors.Is(err, sentinel) {
		t.Errorf("resolve() error = %v, want %v", err, sentinel)
	}
}
