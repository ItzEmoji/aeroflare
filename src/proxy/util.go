package proxy

import (
	"encoding/hex"
	"fmt"
	"strings"

	narhash "aeroflare/src/prepare/hash"
)

// GetProtocol determines http vs https protocol (useful for local mock registry tests)
func GetProtocol(registry string) string {
	if strings.HasPrefix(registry, "127.0.0.1") || strings.HasPrefix(registry, "localhost") {
		return "http"
	}
	return "https"
}

// fileHashToBlobDigest extracts the "FileHash: sha256:<nix-base32>" line from a
// narinfo and returns the equivalent GHCR blob digest "sha256:<hex>".
func fileHashToBlobDigest(narinfo string) (string, error) {
	var fileHash string
	for _, line := range strings.Split(narinfo, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FileHash:") {
			fileHash = strings.TrimSpace(strings.TrimPrefix(line, "FileHash:"))
			break
		}
	}
	if fileHash == "" {
		return "", fmt.Errorf("narinfo has no FileHash line")
	}
	if !strings.HasPrefix(fileHash, "sha256:") {
		return "", fmt.Errorf("unsupported FileHash algorithm: %s", fileHash)
	}
	encoded := strings.TrimPrefix(fileHash, "sha256:")
	raw, err := narhash.DecodeBase32(encoded)
	if err != nil {
		return "", fmt.Errorf("decode nix-base32 FileHash: %w", err)
	}
	return "sha256:" + hex.EncodeToString(raw), nil
}
