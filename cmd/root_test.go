package cmd

import (
	"testing"
	"github.com/spf13/viper"
)

func TestGetCacheURL(t *testing.T) {
	tests := []struct {
		name     string
		cacheUrl string
		cache    string
		expected string
	}{
		{
			name:     "both empty",
			cacheUrl: "",
			cache:    "",
			expected: "",
		},
		{
			name:     "cache-url only",
			cacheUrl: "oci://registry.com/my-cache",
			cache:    "",
			expected: "oci://registry.com/my-cache",
		},
		{
			name:     "cache only",
			cacheUrl: "",
			cache:    "my-org/my-repo",
			expected: "ghcr.io/my-org/my-repo",
		},
		{
			name:     "both set - cache-url takes precedence",
			cacheUrl: "oci://registry.com/my-cache",
			cache:    "my-org/my-repo",
			expected: "oci://registry.com/my-cache",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			if tt.cacheUrl != "" {
				viper.Set("cache-url", tt.cacheUrl)
			}
			if tt.cache != "" {
				viper.Set("cache", tt.cache)
			}
			
			result := GetCacheURL()
			if result != tt.expected {
				t.Errorf("GetCacheURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}
