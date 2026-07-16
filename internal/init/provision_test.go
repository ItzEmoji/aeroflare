package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// buildTarball builds a gzipped tar whose entries are the given path -> content
// pairs, mirroring the layout of a GitHub source archive.
func buildTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", name, err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatalf("write body %q: %v", name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestWorkerScriptFromTarball(t *testing.T) {
	// GitHub wraps the tree in a "<repo>-<tag>" directory, the component the
	// old `tar --strip-components=1` discarded.
	tarball := buildTarball(t, map[string]string{
		"aeroflare-1.9.0/":                                    "",
		"aeroflare-1.9.0/README.md":                           "# readme",
		"aeroflare-1.9.0/proxy/no-webui-native/worker.js":     "export default { fetch() {} }",
		"aeroflare-1.9.0/proxy/no-webui-native/worker.js.map": "should not match",
	})

	got, err := workerScriptFromTarball(bytes.NewReader(tarball))
	if err != nil {
		t.Fatalf("workerScriptFromTarball() error = %v", err)
	}
	if want := "export default { fetch() {} }"; string(got) != want {
		t.Errorf("workerScriptFromTarball() = %q, want %q", got, want)
	}
}

func TestWorkerScriptFromTarballMissingScript(t *testing.T) {
	tarball := buildTarball(t, map[string]string{
		"aeroflare-1.9.0/README.md": "# readme",
	})

	if _, err := workerScriptFromTarball(bytes.NewReader(tarball)); err == nil {
		t.Fatal("workerScriptFromTarball() on an archive without the script: want error, got nil")
	}
}

// A path matching only at the top level must not be mistaken for the real
// entry, which always sits below the archive's wrapper directory.
func TestWorkerScriptFromTarballIgnoresUnwrappedPath(t *testing.T) {
	tarball := buildTarball(t, map[string]string{
		workerScriptRelPath: "unwrapped",
	})

	if _, err := workerScriptFromTarball(bytes.NewReader(tarball)); err == nil {
		t.Fatal("workerScriptFromTarball() on an unwrapped path: want error, got nil")
	}
}

func TestWorkerScriptFromTarballNotGzip(t *testing.T) {
	if _, err := workerScriptFromTarball(strings.NewReader("not a tarball")); err == nil {
		t.Fatal("workerScriptFromTarball() on non-gzip input: want error, got nil")
	}
}
