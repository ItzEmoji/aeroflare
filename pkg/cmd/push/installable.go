package push

import (
	"path/filepath"
	"strings"

	"github.com/itzemoji/aeroflare/internal/ci"
)

// nixStoreDir is the default Nix store location. Installables and result
// symlinks resolve into it.
const nixStoreDir = "/nix/store"

// installableResolver turns positional `push` arguments into realized Nix store
// paths. Each argument is classified as an explicit store path, a local path
// that already points into the store (e.g. a `nix build` result symlink), or a
// flake installable that must be built.
//
// storeDir and build are fields so the classification can be unit-tested
// without a real Nix store or shelling out to `nix build`.
type installableResolver struct {
	storeDir string
	build    func(installable string) ([]string, error)
}

// newInstallableResolver builds a resolver wired to the real Nix store and
// `nix build ... --print-out-paths` (no proxy substituter).
func newInstallableResolver() *installableResolver {
	return &installableResolver{
		storeDir: nixStoreDir,
		build: func(installable string) ([]string, error) {
			return ci.BuildInstallable(installable, 0)
		},
	}
}

// resolve classifies each argument and returns the concatenated store paths in
// argument order.
func (r *installableResolver) resolve(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		// 1. An explicit store path is used as-is.
		if strings.HasPrefix(arg, r.storeDir+"/") {
			out = append(out, arg)
			continue
		}
		// 2. A local path that already resolves into the store (a result
		//    symlink) needs no build; use its top-level store path.
		if storePath, ok := r.storePathFromSymlink(arg); ok {
			out = append(out, storePath)
			continue
		}
		// 3. Anything else is a flake installable and is built.
		paths, err := r.build(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, paths...)
	}
	return out, nil
}

// storePathFromSymlink resolves arg through the filesystem; if it lands inside
// the store it returns the top-level store path (/nix/store/<hash>-<name>),
// trimming any deeper suffix. It returns ok=false when arg does not exist or
// resolves outside the store.
func (r *installableResolver) storePathFromSymlink(arg string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(arg)
	if err != nil {
		return "", false
	}
	prefix := r.storeDir + "/"
	if !strings.HasPrefix(resolved, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(resolved, prefix)
	name := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		name = rest[:i]
	}
	return filepath.Join(r.storeDir, name), true
}
