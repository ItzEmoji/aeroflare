package proxy

import (
	"net/url"
)

// nixBase32Chars contains the specific Base32 alphabet used by Nix.
// Note: Characters like e, o, u, and t are intentionally omitted!
const nixBase32Chars = "0123456789abcdfghijklmnpqrsvwxyz"

// validNixCharMap allows O(1) character validation without regex overhead.
var validNixCharMap [256]bool

func init() {
	for i := 0; i < len(nixBase32Chars); i++ {
		validNixCharMap[nixBase32Chars[i]] = true
	}
}

// IsValidNixStoreHash checks if the provided string is a valid Nix hash.
// This prevents path traversal attacks and catches malformed requests from crawlers or faulty clients.
// Optimized for the hot path by avoiding regex.
func IsValidNixStoreHash(hash string) bool {
	if len(hash) != 32 {
		return false
	}
	for i := 0; i < 32; i++ {
		if !validNixCharMap[hash[i]] {
			return false
		}
	}
	return true
}

// IsValidUpstreamURL ensures that the upstream cache is a valid HTTP/HTTPS URL.
// This is used to fail fast during bootstrap if the configuration is invalid.
func IsValidUpstreamURL(rawURL string) bool {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}

// BuildUpstreamURL safely appends a path to a base URL,
// ensuring there are no issues with duplicate or missing slashes.
func BuildUpstreamURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	// url.JoinPath returns both the joined path and an error.
	joinedPath, err := url.JoinPath(u.Path, path)
	if err != nil {
		return "", err
	}

	u.Path = joinedPath
	return u.String(), nil
}
