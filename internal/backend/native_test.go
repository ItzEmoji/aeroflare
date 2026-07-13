package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/oci"
)

func TestNativeBackend_PushReceipts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aeroflare-native-backend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	narPath := filepath.Join(tmpDir, "test.nar")
	err = os.WriteFile(narPath, []byte("fake nar data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create nar file: %v", err)
	}

	narinfoPath := filepath.Join(tmpDir, "test.narinfo")
	narinfoData := `StorePath: /nix/store/11111111111111111111111111111111-test
URL: test.nar
Compression: none
FileHash: sha256:0j56d7hwksf9zckz58ps8q969h7aak13d9s6n33sjslyfkkb3swx
FileSize: 13
NarHash: sha256:0j56d7hwksf9zckz58ps8q969h7aak13d9s6n33sjslyfkkb3swx
NarSize: 13
References:
Deriver: 22222222222222222222222222222222-test.drv
System: x86_64-linux
Sig: `
	err = os.WriteFile(narinfoPath, []byte(narinfoData), 0644)
	if err != nil {
		t.Fatalf("Failed to create narinfo file: %v", err)
	}

	var mu sync.Mutex
	var checkedBlob, pushedManifest bool

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "HEAD" && strings.HasPrefix(r.URL.Path, "/v2/test-repo/blobs/") {
			mu.Lock()
			checkedBlob = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK) // Simulate blob exists
			return
		}

		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v2/test-repo/manifests/") {
			mu.Lock()
			pushedManifest = true
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")

	cfg := BackendConfig{
		Registry:   u,
		Repository: "test-repo",
		Auth:       oci.BearerAuth("mock-token"),
	}
	backend := &NativeBackend{cfg: cfg}

	receipts := []PushReceipt{
		{
			StorePath:   "/nix/store/11111111111111111111111111111111-test",
			NarinfoPath: narinfoPath,
			NarDigest:   "sha256:fake",
			NarSize:     13,
			NarPath:     narPath,
			IsRoot:      true,
		},
	}

	err = backend.PushReceipts(context.Background(), receipts)
	if err != nil {
		t.Fatalf("PushReceipts failed: %v", err)
	}

	if !checkedBlob {
		t.Errorf("PushReceipts did not check if blob exists")
	}

	if !pushedManifest {
		t.Errorf("PushReceipts did not push manifest")
	}
}
