package network

import "strings"

// GetProtocol returns "http" for localhost/127.0.0.1 registries, and "https" for all others.
func GetProtocol(registry string) string {
	host := registry
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "localhost" || host == "127.0.0.1" {
		return "http"
	}
	return "https"
}