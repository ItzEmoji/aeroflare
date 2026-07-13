package ci

import (
	"fmt"
	"os"
)

// ResolveSigningKey turns a signing-key setting into a filesystem path usable by
// signing.LoadPrivateKey:
//   - ""                    -> ("", noop, nil): no signing.
//   - name of a set env var -> its contents written to a 0600 temp file; cleanup
//     removes it.
//   - otherwise             -> treated as a filesystem path (cleanup is a noop).
func ResolveSigningKey(setting string) (path string, cleanup func(), err error) {
	noop := func() {}
	if setting == "" {
		return "", noop, nil
	}
	if material, ok := os.LookupEnv(setting); ok && material != "" {
		f, err := os.CreateTemp("", "aeroflare-ci-signkey-*")
		if err != nil {
			return "", noop, err
		}
		name := f.Name()
		remove := func() { _ = os.Remove(name) }
		if err := os.Chmod(name, 0o600); err != nil {
			_ = f.Close()
			remove()
			return "", noop, err
		}
		if _, err := f.WriteString(material); err != nil {
			_ = f.Close()
			remove()
			return "", noop, err
		}
		if err := f.Close(); err != nil {
			remove()
			return "", noop, err
		}
		return name, remove, nil
	}
	if _, statErr := os.Stat(setting); statErr != nil {
		return "", noop, fmt.Errorf("signing key %q is neither a set env var nor an existing file", setting)
	}
	return setting, noop, nil
}
