package root

import (
	"testing"

	"github.com/spf13/viper"
)

func TestResolveCacheURL(t *testing.T) {
	tests := []struct {
		name     string
		cacheURL string
		cache    string
		expected string
	}{
		{name: "both empty", cacheURL: "", cache: "", expected: ""},
		{name: "cache-url wins", cacheURL: "oci://example.com/foo", cache: "org/repo", expected: "oci://example.com/foo"},
		{name: "cache shorthand expands to ghcr.io", cacheURL: "", cache: "org/repo", expected: "ghcr.io/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.Set("cache-url", tt.cacheURL)
			v.Set("cache", tt.cache)

			if got := ResolveCacheURL(v); got != tt.expected {
				t.Errorf("ResolveCacheURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}
