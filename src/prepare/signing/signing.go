package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PrivateKey holds an Ed25519 private key for signing narinfo files.
// It is safe for concurrent use.
type PrivateKey struct {
	Name string
	key  ed25519.PrivateKey
}

// PublicKey holds an Ed25519 public key for verifying narinfo signatures.
type PublicKey struct {
	Name string
	key  ed25519.PublicKey
}

// LoadPrivateKey reads a Nix signing key file and returns a PrivateKey.
// The file format is: "key-name:base64-encoded-32-byte-seed"
// This is the format produced by `nix key-gen-secret`.
func LoadPrivateKey(path string) (*PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}

	return ParsePrivateKey(string(data))
}

// ParsePrivateKey parses a Nix signing key string and returns a PrivateKey.
// The format is: "key-name:base64-encoded-32-byte-seed"
func ParsePrivateKey(s string) (*PrivateKey, error) {
	s = strings.TrimSpace(s)
	idx := strings.Index(s, ":")
	if idx < 0 {
		return nil, fmt.Errorf("invalid signing key format: expected 'name:base64seed'")
	}

	name := s[:idx]
	seedB64 := s[idx+1:]

	if name == "" {
		return nil, fmt.Errorf("invalid signing key: empty key name")
	}

	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil {
		return nil, fmt.Errorf("decode signing key seed: %w", err)
	}

	var privKey ed25519.PrivateKey
	switch len(seed) {
	case ed25519.SeedSize:
		privKey = ed25519.NewKeyFromSeed(seed)
	case ed25519.PrivateKeySize:
		privKey = ed25519.PrivateKey(seed)
	default:
		return nil, fmt.Errorf("invalid signing key seed: expected %d or %d bytes, got %d", ed25519.SeedSize, ed25519.PrivateKeySize, len(seed))
	}

	return &PrivateKey{
		Name: name,
		key:  privKey,
	}, nil
}

// PublicKey returns the Ed25519 public key derived from this private key.
func (pk *PrivateKey) PublicKey() *PublicKey {
	pub := pk.key.Public().(ed25519.PublicKey)
	return &PublicKey{
		Name: pk.Name,
		key:  pub,
	}
}

// Fingerprint computes the Nix narinfo fingerprint string that gets signed.
// The format is: "1;storePath;narHash;narSize;ref1;ref2;..."
// where references are the basenames (e.g. "abc123-glibc-2.37").
func Fingerprint(storePath, narHash string, narSize int64, references []string) string {
	storeDir := filepath.Dir(storePath)

	var absoluteRefs []string
	for _, ref := range references {
		if filepath.IsAbs(ref) {
			absoluteRefs = append(absoluteRefs, ref)
		} else {
			absoluteRefs = append(absoluteRefs, fmt.Sprintf("%s/%s", storeDir, ref))
		}
	}

	sort.Strings(absoluteRefs)

	parts := []string{
		"1",
		storePath,
		narHash,
		strconv.FormatInt(narSize, 10),
		strings.Join(absoluteRefs, ","),
	}
	return strings.Join(parts, ";")
}

// Sign signs a fingerprint string and returns the signature in Nix format:
// "key-name:base64-signature"
func (pk *PrivateKey) Sign(fingerprint string) string {
	sig := ed25519.Sign(pk.key, []byte(fingerprint))
	return fmt.Sprintf("%s:%s", pk.Name, base64.StdEncoding.EncodeToString(sig))
}

// SignNarinfo computes the fingerprint for the given narinfo fields, signs it,
// and returns the signature string. It does not modify the narinfo.
func (pk *PrivateKey) SignNarinfo(storePath, narHash string, narSize int64, references []string) string {
	fp := Fingerprint(storePath, narHash, narSize, references)
	return pk.Sign(fp)
}

// ParsePublicKey parses a Nix public key string and returns a PublicKey.
// The format is: "key-name:base64-encoded-32-byte-public-key"
func ParsePublicKey(s string) (*PublicKey, error) {
	s = strings.TrimSpace(s)
	idx := strings.Index(s, ":")
	if idx < 0 {
		return nil, fmt.Errorf("invalid public key format: expected 'name:base64key'")
	}

	name := s[:idx]
	keyB64 := s[idx+1:]

	if name == "" {
		return nil, fmt.Errorf("invalid public key: empty key name")
	}

	keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}

	if len(keyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key: expected %d bytes, got %d", ed25519.PublicKeySize, len(keyBytes))
	}

	return &PublicKey{
		Name: name,
		key:  ed25519.PublicKey(keyBytes),
	}, nil
}

// String returns the Nix-format public key string: "key-name:base64-key"
func (pk *PublicKey) String() string {
	return fmt.Sprintf("%s:%s", pk.Name, base64.StdEncoding.EncodeToString(pk.key))
}

// Verify checks whether a signature is valid for the given fingerprint.
// The signature should be in "key-name:base64-signature" format.
func (pk *PublicKey) Verify(fingerprint, sigStr string) error {
	idx := strings.Index(sigStr, ":")
	if idx < 0 {
		return fmt.Errorf("invalid signature format: expected 'name:base64sig'")
	}

	sigName := sigStr[:idx]
	if sigName != pk.Name {
		return fmt.Errorf("signature key name %q does not match expected %q", sigName, pk.Name)
	}

	sigB64 := sigStr[idx+1:]
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(pk.key, []byte(fingerprint), sig) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// VerifyNarinfo verifies a narinfo signature against the given narinfo fields.
func (pk *PublicKey) VerifyNarinfo(storePath, narHash string, narSize int64, references []string, sigStr string) error {
	fp := Fingerprint(storePath, narHash, narSize, references)
	return pk.Verify(fp, sigStr)
}
