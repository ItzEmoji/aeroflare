package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aeroflare/src"
)

func TestExchangeToken(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token": "my-bearer-token-123"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	token, err := network.ExchangeToken(u, "test-repo/nix-cache", "my-basic-auth-pat")
	if err != nil {
		t.Fatalf("ExchangeToken failed: %v", err)
	}

	if token != "my-bearer-token-123" {
		t.Errorf("Expected my-bearer-token-123, got %s", token)
	}
}

func TestPushAndPullBlob(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aeroflare-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFilePath := filepath.Join(tmpDir, "test.txt")
	testContent := "hello nix binary cache"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	var checkedBlob, initiatedUpload, uploadedBlob bool

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock HEAD blobs checks
		if r.Method == "HEAD" && strings.HasPrefix(r.URL.Path, "/v2/test-repo/blobs/") {
			checkedBlob = true
			w.WriteHeader(http.StatusNotFound) // Simulate blob does not exist yet
			return
		}

		// Mock initiate upload POST
		if r.Method == "POST" && r.URL.Path == "/v2/test-repo/blobs/uploads/" {
			initiatedUpload = true
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/session-123")
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Mock PUT blob upload
		if r.Method == "PUT" && r.URL.Path == "/v2/test-repo/blobs/uploads/session-123" {
			uploadedBlob = true
			w.WriteHeader(http.StatusCreated)
			return
		}

		// Mock GET blobs pull
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/test-repo/blobs/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(testContent))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")

	// 1. Push
	digest, err := network.PushBlob(testFilePath, u, "test-repo", "mock-token")
	if err != nil {
		t.Fatalf("PushBlob failed: %v", err)
	}

	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("Expected digest starting with sha256:, got %s", digest)
	}
	if !checkedBlob || !initiatedUpload || !uploadedBlob {
		t.Errorf("PushBlob did not trigger all expected registry steps: checked=%v, initiated=%v, uploaded=%v", checkedBlob, initiatedUpload, uploadedBlob)
	}

	// 2. Pull
	outFilePath := filepath.Join(tmpDir, "out.txt")
	err = network.PullBlob(digest, outFilePath, u, "test-repo", "mock-token")
	if err != nil {
		t.Fatalf("PullBlob failed: %v", err)
	}

	pulledData, err := os.ReadFile(outFilePath)
	if err != nil {
		t.Fatalf("Failed to read pulled file: %v", err)
	}

	if string(pulledData) != testContent {
		t.Errorf("Expected %s, got %s", testContent, string(pulledData))
	}
}
