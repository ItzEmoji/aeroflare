package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func generateTestKey(t *testing.T, name string) (*PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := priv.Seed()
	seedB64 := base64.StdEncoding.EncodeToString(seed)
	keyStr := name + ":" + seedB64

	pk, err := ParsePrivateKey(keyStr)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the public key matches
	derived := pk.PublicKey()
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	expectedPubB64 := base64.StdEncoding.EncodeToString(derived.key)
	if pubB64 != expectedPubB64 {
		t.Fatalf("public key mismatch: derived %s, expected %s", expectedPubB64, pubB64)
	}

	return pk, keyStr
}

func TestParsePrivateKey(t *testing.T) {
	pk, _ := generateTestKey(t, "test-cache")

	if pk.Name != "test-cache" {
		t.Errorf("Name = %s, want test-cache", pk.Name)
	}
	if len(pk.key) != ed25519.PrivateKeySize {
		t.Errorf("private key length = %d, want %d", len(pk.key), ed25519.PrivateKeySize)
	}
}

func TestParsePrivateKeyInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no separator", "justakey"},
		{"empty name", ":base64data"},
		{"invalid base64", "test:not-base64!!!"},
		{"wrong seed size", "test:" + base64.StdEncoding.EncodeToString([]byte("short"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePrivateKey(tt.input)
			if err == nil {
				t.Errorf("expected error for %q", tt.input)
			}
		})
	}
}

func TestLoadPrivateKey(t *testing.T) {
	_, keyStr := generateTestKey(t, "my-cache")

	tmpFile := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(tmpFile, []byte(keyStr), 0o600); err != nil {
		t.Fatal(err)
	}

	pk, err := LoadPrivateKey(tmpFile)
	if err != nil {
		t.Fatalf("LoadPrivateKey error: %v", err)
	}
	if pk.Name != "my-cache" {
		t.Errorf("Name = %s, want my-cache", pk.Name)
	}
}

func TestFingerprint(t *testing.T) {
	fp := Fingerprint(
		"/nix/store/abc-hello-2.10",
		"sha256:xyz123",
		67890,
		[]string{"def-glibc-2.37", "ghi-zlib-1.2"},
	)

	expected := "1;/nix/store/abc-hello-2.10;sha256:xyz123;67890;def-glibc-2.37;ghi-zlib-1.2"
	if fp != expected {
		t.Errorf("Fingerprint = %q, want %q", fp, expected)
	}
}

func TestFingerprintNoReferences(t *testing.T) {
	fp := Fingerprint(
		"/nix/store/abc-hello-2.10",
		"sha256:xyz123",
		67890,
		nil,
	)

	expected := "1;/nix/store/abc-hello-2.10;sha256:xyz123;67890"
	if fp != expected {
		t.Errorf("Fingerprint = %q, want %q", fp, expected)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pk, _ := generateTestKey(t, "test-cache")

	storePath := "/nix/store/abc-hello-2.10"
	narHash := "sha256:xyz123"
	narSize := int64(67890)
	references := []string{"def-glibc-2.37"}

	sigStr := pk.SignNarinfo(storePath, narHash, narSize, references)

	// Sig should be "test-cache:base64sig"
	if sigStr[:11] != "test-cache:" {
		t.Errorf("signature should start with key name, got %s", sigStr[:11])
	}

	// Verify with the public key
	pub := pk.PublicKey()
	err := pub.VerifyNarinfo(storePath, narHash, narSize, references, sigStr)
	if err != nil {
		t.Fatalf("VerifyNarinfo error: %v", err)
	}
}

func TestVerifyInvalidSignature(t *testing.T) {
	pk, _ := generateTestKey(t, "test-cache")

	storePath := "/nix/store/abc-hello-2.10"
	narHash := "sha256:xyz123"
	narSize := int64(67890)
	references := []string{"def-glibc-2.37"}

	sigStr := pk.SignNarinfo(storePath, narHash, narSize, references)

	// Verify with wrong data (different store path)
	pub := pk.PublicKey()
	err := pub.VerifyNarinfo("/nix/store/WRONG-path", narHash, narSize, references, sigStr)
	if err == nil {
		t.Error("expected verification error for wrong store path")
	}
}

func TestVerifyWrongKeyName(t *testing.T) {
	pk1, _ := generateTestKey(t, "cache1")
	pk2, _ := generateTestKey(t, "cache2")

	sigStr := pk1.SignNarinfo("/nix/store/abc", "sha256:xyz", 100, nil)

	// Verify with a different key name
	pub2 := pk2.PublicKey()
	err := pub2.VerifyNarinfo("/nix/store/abc", "sha256:xyz", 100, nil, sigStr)
	if err == nil {
		t.Error("expected error for mismatched key name")
	}
}

func TestPublicKeyString(t *testing.T) {
	pk, _ := generateTestKey(t, "my-cache")
	pub := pk.PublicKey()

	pubStr := pub.String()
	if pubStr[:9] != "my-cache:" {
		t.Errorf("public key string should start with key name, got %s", pubStr[:9])
	}

	// Parse it back
	pub2, err := ParsePublicKey(pubStr)
	if err != nil {
		t.Fatalf("ParsePublicKey error: %v", err)
	}
	if pub2.Name != "my-cache" {
		t.Errorf("Name = %s, want my-cache", pub2.Name)
	}
}

func TestDeterministicSigning(t *testing.T) {
	pk, _ := generateTestKey(t, "test-cache")

	// Same input should produce the same signature (Ed25519 is deterministic)
	sig1 := pk.SignNarinfo("/nix/store/abc", "sha256:xyz", 100, []string{"ref1"})
	sig2 := pk.SignNarinfo("/nix/store/abc", "sha256:xyz", 100, []string{"ref1"})

	if sig1 != sig2 {
		t.Error("Ed25519 signing should be deterministic")
	}
}

func TestSignNarinfoWithEmptyReferences(t *testing.T) {
	pk, _ := generateTestKey(t, "test-cache")
	pub := pk.PublicKey()

	storePath := "/nix/store/abc-hello-2.10"
	narHash := "sha256:xyz"
	narSize := int64(100)

	sigStr := pk.SignNarinfo(storePath, narHash, narSize, nil)

	err := pub.VerifyNarinfo(storePath, narHash, narSize, nil, sigStr)
	if err != nil {
		t.Fatalf("VerifyNarinfo with empty refs error: %v", err)
	}
}

func TestDifferentKeysProduceDifferentSignatures(t *testing.T) {
	pk1, _ := generateTestKey(t, "cache1")
	pk2, _ := generateTestKey(t, "cache2")

	storePath := "/nix/store/abc"
	narHash := "sha256:xyz"
	narSize := int64(100)

	sig1 := pk1.SignNarinfo(storePath, narHash, narSize, nil)
	sig2 := pk2.SignNarinfo(storePath, narHash, narSize, nil)

	if sig1 == sig2 {
		t.Error("different keys should produce different signatures")
	}
}
