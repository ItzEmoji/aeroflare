package oci

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetProtocol verifies that localhost/127.0.0.1 registries use http, others use https.
func TestGetProtocol(t *testing.T) {
	cases := []struct {
		registry string
		expected string
	}{
		{"127.0.0.1", "http"},
		{"127.0.0.1:5000", "http"},
		{"localhost", "http"},
		{"localhost:5000", "http"},
		{"ghcr.io", "https"},
		{"registry.hub.docker.com", "https"},
		{"my.private.registry", "https"},
		{"[::1]:5000", "http"},
		{"[::1]", "http"},
		// A hostname merely starting with "localhost" is NOT local.
		{"localhost.example.com", "https"},
	}

	for _, tc := range cases {
		got := GetProtocol(tc.registry)
		if got != tc.expected {
			t.Errorf("GetProtocol(%q) = %q, want %q", tc.registry, got, tc.expected)
		}
	}
}

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
	token, err := ExchangeToken(u, "test-repo", "token", "my-basic-auth-pat")
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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFilePath := filepath.Join(tmpDir, "test.txt")
	testContent := "hello nix binary cache"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	var checkedBlob, initiatedUpload, uploadedBlob bool

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock API Ping
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}

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

		// Mock PATCH blob upload
		if r.Method == "PATCH" && r.URL.Path == "/v2/test-repo/blobs/uploads/session-123" {
			w.Header().Set("Location", "/v2/test-repo/blobs/uploads/session-123")
			w.WriteHeader(http.StatusAccepted)
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
	digest, err := PushBlob(testFilePath, u, "test-repo", "mock-token")
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
	err = PullBlob(digest, outFilePath, u, "test-repo", "mock-token")
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

// TestExchangeToken_Error verifies that a non-200 response returns an error.
func TestExchangeToken_Error(t *testing.T) {
	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	_, err := ExchangeToken(u, "test-repo", "token", "bad-token")
	if err == nil {
		t.Fatal("Expected error for 401 response, got nil")
	}
}

// TestPushBlob_AlreadyExists verifies that PushBlob skips upload when blob already exists (HEAD returns 200).
func TestPushBlob_AlreadyExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aeroflare-test-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFilePath := filepath.Join(tmpDir, "existing.txt")
	if err := os.WriteFile(testFilePath, []byte("already uploaded content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	var uploadAttempted bool

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock API Ping
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "HEAD" && strings.HasPrefix(r.URL.Path, "/v2/test-repo/blobs/") {
			w.WriteHeader(http.StatusOK) // Blob already exists
			return
		}
		// Any upload attempt should not happen
		if r.Method == "POST" || r.Method == "PUT" {
			uploadAttempted = true
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	digest, err := PushBlob(testFilePath, u, "test-repo", "mock-token")
	if err != nil {
		t.Fatalf("PushBlob failed: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("Expected digest starting with sha256:, got %s", digest)
	}
	if uploadAttempted {
		t.Error("PushBlob should not attempt upload when blob already exists")
	}
}

// TestPullBlob_Error verifies that PullBlob returns an error on a non-200 response.
func TestPullBlob_Error(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aeroflare-test-pull-err-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mockRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock API Ping
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("blob not found"))
	}))
	defer mockRegistry.Close()

	u := strings.TrimPrefix(mockRegistry.URL, "http://")
	outFilePath := filepath.Join(tmpDir, "out.txt")
	err = PullBlob("sha256:nonexistentdigest", outFilePath, u, "test-repo", "mock-token")
	if err == nil {
		t.Fatal("Expected error for 404 response, got nil")
	}
}

// TestExchangeToken_UsesHttpForLocalhost verifies that ExchangeToken uses http:// for localhost registries.
func TestExchangeToken_UsesHttpForLocalhost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token": "localhost-token"}`))
	}))
	defer ts.Close()

	u := strings.TrimPrefix(ts.URL, "http://")

	token, err := ExchangeToken(u, "my-org", "token", "test-pat")
	if err != nil {
		t.Fatalf("ExchangeToken failed for localhost registry: %v", err)
	}
	if token != "localhost-token" {
		t.Errorf("expected localhost-token, got %s", token)
	}
}
