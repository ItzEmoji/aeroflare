package prepare

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/narinfo"
	"aeroflare/src/prepare/signing"
	"aeroflare/src/prepare/store"
)

func TestWriteNarAndNarinfo(t *testing.T) {
	// Create a fake nix store path (a directory with files)
	storePath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storePath, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storePath, "share", "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "bin", "hello"), []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "share", "doc", "hello.txt"), []byte("Hello World\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &store.PathInfo{
		Path:   storePath,
		Hash:   "testhash123",
		Name:   "hello-2.10",
		System: "x86_64-linux",
	}

	outputDir := t.TempDir()
	cfg := &Config{
		OutputDir:   outputDir,
		Compression: compress.Zstd,
	}

	narPath, narinfoPath, err := writeNarAndNarinfo(storePath, "testhash123", info, cfg)
	if err != nil {
		t.Fatalf("writeNarAndNarinfo error: %v", err)
	}

	// Verify NAR file exists
	if _, err := os.Stat(narPath); err != nil {
		t.Errorf("NAR file not created: %v", err)
	}
	if !strings.HasSuffix(narPath, ".nar.zst") {
		t.Errorf("expected .nar.zst extension, got %s", narPath)
	}

	// Verify narinfo file exists and has correct content
	narinfoData, err := os.ReadFile(narinfoPath)
	if err != nil {
		t.Fatalf("read narinfo: %v", err)
	}

	ni, err := narinfo.Parse(string(narinfoData))
	if err != nil {
		t.Fatalf("parse narinfo: %v", err)
	}

	if ni.StorePath != storePath {
		t.Errorf("StorePath: got %s, want %s", ni.StorePath, storePath)
	}
	if ni.Compression != "zstd" {
		t.Errorf("Compression: got %s, want zstd", ni.Compression)
	}
	if !strings.HasPrefix(ni.URL, "nar/testhash123.nar.zst") {
		t.Errorf("URL: got %s", ni.URL)
	}
	if !strings.HasPrefix(ni.NarHash, "sha256:") {
		t.Errorf("NarHash should start with sha256:, got %s", ni.NarHash)
	}
	if ni.NarSize == 0 {
		t.Error("NarSize should be non-zero")
	}
	if !strings.HasPrefix(ni.FileHash, "sha256:") {
		t.Errorf("FileHash should start with sha256:, got %s", ni.FileHash)
	}
	if ni.FileSize == 0 {
		t.Error("FileSize should be non-zero")
	}
	if ni.System != "x86_64-linux" {
		t.Errorf("System: got %s, want x86_64-linux", ni.System)
	}
}

func TestWriteNarAndNarinfoWithReferences(t *testing.T) {
	storePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(storePath, "lib.so"), []byte("fake lib"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &store.PathInfo{
		Path:       storePath,
		Hash:       "refhash456",
		Name:       "mylib-1.0",
		References: []string{"/nix/store/aaa111-glibc-2.37", "/nix/store/bbb222-zlib-1.2"},
		Deriver:    "/nix/store/ccc333-mylib-1.0.drv",
		System:     "x86_64-linux",
	}

	outputDir := t.TempDir()
	cfg := &Config{
		OutputDir:   outputDir,
		Compression: compress.Xz,
	}

	_, narinfoPath, err := writeNarAndNarinfo(storePath, "refhash456", info, cfg)
	if err != nil {
		t.Fatalf("writeNarAndNarinfo error: %v", err)
	}

	narinfoData, err := os.ReadFile(narinfoPath)
	if err != nil {
		t.Fatalf("read narinfo: %v", err)
	}

	ni, err := narinfo.Parse(string(narinfoData))
	if err != nil {
		t.Fatalf("parse narinfo: %v", err)
	}

	if len(ni.References) != 2 {
		t.Fatalf("expected 2 references, got %d: %v", len(ni.References), ni.References)
	}
	if ni.References[0] != "aaa111-glibc-2.37" {
		t.Errorf("Reference[0]: got %s, want aaa111-glibc-2.37", ni.References[0])
	}
	if ni.References[1] != "bbb222-zlib-1.2" {
		t.Errorf("Reference[1]: got %s, want bbb222-zlib-1.2", ni.References[1])
	}
	if ni.Deriver != "ccc333-mylib-1.0.drv" {
		t.Errorf("Deriver: got %s, want ccc333-mylib-1.0.drv", ni.Deriver)
	}
	if ni.Compression != "xz" {
		t.Errorf("Compression: got %s, want xz", ni.Compression)
	}
}

func TestCollectReferenceHashes(t *testing.T) {
	infos := map[string]*store.PathInfo{
		"/nix/store/aaa-pkg1": {
			Path:       "/nix/store/aaa-pkg1",
			Hash:       "aaa",
			References: []string{"/nix/store/bbb-dep1", "/nix/store/ccc-dep2"},
		},
		"/nix/store/ddd-pkg2": {
			Path:       "/nix/store/ddd-pkg2",
			Hash:       "ddd",
			References: []string{"/nix/store/ccc-dep2", "/nix/store/eee-dep3"},
		},
	}

	hashes, hashToPath := collectReferenceHashes(infos)

	if len(hashes) != 3 {
		t.Errorf("expected 3 unique reference hashes, got %d: %v", len(hashes), hashes)
	}

	// Verify dedup: ccc-dep2 appears in both packages but should only be collected once
	hashSet := make(map[string]bool)
	for _, h := range hashes {
		hashSet[h] = true
	}
	expected := map[string]bool{"bbb": true, "ccc": true, "eee": true}
	for h := range expected {
		if !hashSet[h] {
			t.Errorf("expected hash %s to be collected", h)
		}
	}

	// Verify hashToPath mapping
	if hashToPath["bbb"] != "/nix/store/bbb-dep1" {
		t.Errorf("hashToPath[bbb] = %s, want /nix/store/bbb-dep1", hashToPath["bbb"])
	}
}

func TestComputeMissingRefs(t *testing.T) {
	references := []string{
		"/nix/store/aaa-pkg1",
		"/nix/store/bbb-pkg2",
		"/nix/store/ccc-pkg3",
	}
	hashToPath := map[string]string{
		"aaa": "/nix/store/aaa-pkg1",
		"bbb": "/nix/store/bbb-pkg2",
		"ccc": "/nix/store/ccc-pkg3",
	}

	// Case 1: Some references exist on cache
	existsMap := map[string]bool{
		"aaa": true,
		"bbb": false,
		"ccc": true,
	}
	missing := computeMissingRefs(references, existsMap, hashToPath)
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing ref, got %d: %v", len(missing), missing)
	}
	if missing[0] != "/nix/store/bbb-pkg2" {
		t.Errorf("missing[0] = %s, want /nix/store/bbb-pkg2", missing[0])
	}

	// Case 2: No cache configured (empty existsMap) -> all refs are missing
	missing = computeMissingRefs(references, map[string]bool{}, hashToPath)
	if len(missing) != 3 {
		t.Errorf("expected 3 missing refs with no cache, got %d", len(missing))
	}
}

func TestPrepareWithMockCache(t *testing.T) {
	// Set up a mock binary cache server
	existingHashes := map[string]bool{
		"dep1hash": true,
		"dep2hash": false, // this one is missing
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hashPath := r.URL.Path[1:]
		hash := hashPath[:len(hashPath)-len(".narinfo")]
		if existingHashes[hash] {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Test the reference checking logic directly
	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		CacheURL:    server.URL,
		Workers:     5,
	}

	ctx := context.Background()
	references := []string{
		"/nix/store/dep1hash-dep1-1.0",
		"/nix/store/dep2hash-dep2-2.0",
	}

	missing, err := checkReferences(ctx, references, cfg)
	if err != nil {
		t.Fatalf("checkReferences error: %v", err)
	}

	if len(missing) != 1 {
		t.Fatalf("expected 1 missing reference, got %d: %v", len(missing), missing)
	}
	if missing[0] != "/nix/store/dep2hash-dep2-2.0" {
		t.Errorf("missing[0] = %s, want /nix/store/dep2hash-dep2-2.0", missing[0])
	}
}

func TestPrepareAllCompressionTypes(t *testing.T) {
	storePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(storePath, "file.txt"), []byte("test content"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &store.PathInfo{
		Path:   storePath,
		Hash:   "comp123",
		Name:   "test-pkg",
		System: "x86_64-linux",
	}

	for _, comp := range []compress.Type{compress.None, compress.Gzip, compress.Xz, compress.Zstd} {
		t.Run(string(comp), func(t *testing.T) {
			cfg := &Config{
				OutputDir:   t.TempDir(),
				Compression: comp,
			}
			narPath, narinfoPath, err := writeNarAndNarinfo(storePath, "comp123", info, cfg)
			if err != nil {
				t.Fatalf("writeNarAndNarinfo error: %v", err)
			}

			if _, err := os.Stat(narPath); err != nil {
				t.Errorf("NAR file not created: %v", err)
			}
			if _, err := os.Stat(narinfoPath); err != nil {
				t.Errorf("narinfo file not created: %v", err)
			}

			data, err := os.ReadFile(narinfoPath)
			if err != nil {
				t.Fatal(err)
			}
			ni, err := narinfo.Parse(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if ni.Compression != string(comp) {
				t.Errorf("Compression: got %s, want %s", ni.Compression, comp)
			}
		})
	}
}

func TestParseInputFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "paths.txt")
	content := `# This is a comment
/nix/store/aaa-pkg1
/nix/store/bbb-pkg2

# Another comment
/nix/store/ccc-pkg3
`
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("ParseInputFile error: %v", err)
	}

	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
	}
	expected := []string{
		"/nix/store/aaa-pkg1",
		"/nix/store/bbb-pkg2",
		"/nix/store/ccc-pkg3",
	}
	for i, p := range paths {
		if p != expected[i] {
			t.Errorf("path[%d] = %s, want %s", i, p, expected[i])
		}
	}
}

func TestPrepareRefsOnly(t *testing.T) {
	// Create fake store paths for the missing refs
	ref1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(ref1, "lib.so"), []byte("lib1 content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(ref2, "lib.so"), []byte("lib2 content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		Workers:     5,
	}

	// Use writeNarsFromInfos directly (bypasses nix command calls)
	infos := map[string]*store.PathInfo{
		ref1: {Path: ref1, Hash: "ref1hash", Name: "dep1", System: "x86_64-linux"},
		ref2: {Path: ref2, Hash: "ref2hash", Name: "dep2", System: "x86_64-linux"},
	}

	results, err := writeNarsFromInfos([]string{ref1, ref2}, infos, cfg)
	if err != nil {
		t.Fatalf("writeNarsFromInfos error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.NarPath == "" {
			t.Error("expected non-empty NarPath")
		}
		if r.NarinfoPath == "" {
			t.Error("expected non-empty NarinfoPath")
		}
		if _, err := os.Stat(r.NarPath); err != nil {
			t.Errorf("NAR file not created: %v", err)
		}
		if _, err := os.Stat(r.NarinfoPath); err != nil {
			t.Errorf("narinfo file not created: %v", err)
		}
		// MissingRefs should be empty since we don't check refs
		if len(r.MissingRefs) != 0 {
			t.Errorf("expected no missing refs, got %v", r.MissingRefs)
		}
	}
}

func TestPrepareRefsOnlyDedup(t *testing.T) {
	ref := t.TempDir()
	if err := os.WriteFile(filepath.Join(ref, "file"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		Workers:     2,
	}

	infos := map[string]*store.PathInfo{
		ref: {Path: ref, Hash: "refhash", Name: "dep", System: "x86_64-linux"},
	}

	// Test that writeNarsFromInfos handles a single path correctly
	// (dedup happens in prepareRefsOnly before calling this)
	results, err := writeNarsFromInfos([]string{ref}, infos, cfg)
	if err != nil {
		t.Fatalf("writeNarsFromInfos error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestPrepareWithMissingRefsPrepared(t *testing.T) {
	// Create a real ref path that will be "missing" from cache.
	// Use a nix-store-formatted name so store.ParsePath can extract the hash.
	fakeStoreRoot := t.TempDir()
	missingRef := filepath.Join(fakeStoreRoot, "dep2hash-dep2-2.0")
	if err := os.MkdirAll(missingRef, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missingRef, "lib.so"), []byte("lib content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mock cache: "dep1hash" exists, "dep2hash" does not
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hashPath := r.URL.Path[1:]
		hash := hashPath[:len(hashPath)-len(".narinfo")]
		if hash == "dep1hash" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &Config{
		OutputDir:          t.TempDir(),
		Compression:        compress.Zstd,
		CacheURL:           server.URL,
		Workers:            5,
		PrepareMissingRefs: true,
	}

	ctx := context.Background()

	// Simulate: check references, then prepare the missing ones
	references := []string{
		"/nix/store/dep1hash-dep1-1.0",
		missingRef, // this is a real path that will be "missing" from cache
	}

	missing, err := checkReferences(ctx, references, cfg)
	if err != nil {
		t.Fatalf("checkReferences error: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing ref, got %d: %v", len(missing), missing)
	}

	// Prepare the missing ref using writeNarsFromInfos (bypasses nix commands)
	infos := map[string]*store.PathInfo{
		missingRef: {Path: missingRef, Hash: "dep2hash", Name: "dep2-2.0", System: "x86_64-linux"},
	}
	refResults, err := writeNarsFromInfos(missing, infos, cfg)
	if err != nil {
		t.Fatalf("writeNarsFromInfos error: %v", err)
	}
	if len(refResults) != 1 {
		t.Fatalf("expected 1 ref result, got %d", len(refResults))
	}

	// Verify the missing ref got a NAR and narinfo
	rr := refResults[0]
	if _, err := os.Stat(rr.NarPath); err != nil {
		t.Errorf("missing ref NAR not created: %v", err)
	}
	if _, err := os.Stat(rr.NarinfoPath); err != nil {
		t.Errorf("missing ref narinfo not created: %v", err)
	}
}

func generateTestSigningKey(t *testing.T, name string) *signing.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := priv.Seed()
	keyStr := name + ":" + base64.StdEncoding.EncodeToString(seed)
	pk, err := signing.ParsePrivateKey(keyStr)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestWriteNarAndNarinfoWithSigning(t *testing.T) {
	storePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(storePath, "file.txt"), []byte("signed content"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &store.PathInfo{
		Path:   storePath,
		Hash:   "signedhash",
		Name:   "test-pkg",
		System: "x86_64-linux",
	}

	signKey := generateTestSigningKey(t, "my-cache")

	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		SigningKey:  signKey,
	}

	_, narinfoPath, err := writeNarAndNarinfo(storePath, "signedhash", info, cfg)
	if err != nil {
		t.Fatalf("writeNarAndNarinfo error: %v", err)
	}

	narinfoData, err := os.ReadFile(narinfoPath)
	if err != nil {
		t.Fatalf("read narinfo: %v", err)
	}

	ni, err := narinfo.Parse(string(narinfoData))
	if err != nil {
		t.Fatalf("parse narinfo: %v", err)
	}

	if ni.Sig == "" {
		t.Fatal("expected non-empty Sig field")
	}

	// Sig should be "my-cache:base64signature"
	if !strings.HasPrefix(ni.Sig, "my-cache:") {
		t.Errorf("Sig should start with 'my-cache:', got %s", ni.Sig)
	}

	// Verify the signature
	pub := signKey.PublicKey()
	err = pub.VerifyNarinfo(ni.StorePath, ni.NarHash, ni.NarSize, ni.References, ni.Sig)
	if err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestWriteNarAndNarinfoWithoutSigning(t *testing.T) {
	storePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(storePath, "file.txt"), []byte("unsigned content"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &store.PathInfo{
		Path:   storePath,
		Hash:   "unsignedhash",
		Name:   "test-pkg",
		System: "x86_64-linux",
	}

	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		// No SigningKey
	}

	_, narinfoPath, err := writeNarAndNarinfo(storePath, "unsignedhash", info, cfg)
	if err != nil {
		t.Fatalf("writeNarAndNarinfo error: %v", err)
	}

	narinfoData, err := os.ReadFile(narinfoPath)
	if err != nil {
		t.Fatalf("read narinfo: %v", err)
	}

	ni, err := narinfo.Parse(string(narinfoData))
	if err != nil {
		t.Fatalf("parse narinfo: %v", err)
	}

	if ni.Sig != "" {
		t.Errorf("expected empty Sig when no signing key, got %s", ni.Sig)
	}
}

func TestWriteNarsFromInfosWithSigning(t *testing.T) {
	ref1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(ref1, "lib.so"), []byte("lib1"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(ref2, "lib.so"), []byte("lib2"), 0o644); err != nil {
		t.Fatal(err)
	}

	signKey := generateTestSigningKey(t, "batch-cache")
	pub := signKey.PublicKey()

	cfg := &Config{
		OutputDir:   t.TempDir(),
		Compression: compress.Zstd,
		Workers:     2,
		SigningKey:  signKey,
	}

	infos := map[string]*store.PathInfo{
		ref1: {Path: ref1, Hash: "ref1hash", Name: "dep1", System: "x86_64-linux"},
		ref2: {Path: ref2, Hash: "ref2hash", Name: "dep2", System: "x86_64-linux"},
	}

	results, err := writeNarsFromInfos([]string{ref1, ref2}, infos, cfg)
	if err != nil {
		t.Fatalf("writeNarsFromInfos error: %v", err)
	}

	for _, r := range results {
		if !r.Signed {
			t.Errorf("expected Signed=true for %s", r.StorePath)
		}

		data, err := os.ReadFile(r.NarinfoPath)
		if err != nil {
			t.Fatalf("read narinfo: %v", err)
		}
		ni, err := narinfo.Parse(string(data))
		if err != nil {
			t.Fatalf("parse narinfo: %v", err)
		}
		if ni.Sig == "" {
			t.Fatal("expected non-empty Sig")
		}
		err = pub.VerifyNarinfo(ni.StorePath, ni.NarHash, ni.NarSize, ni.References, ni.Sig)
		if err != nil {
			t.Fatalf("signature verification failed for %s: %v", r.StorePath, err)
		}
	}
}
