package cmd

import (
	"testing"
	"github.com/spf13/viper"
	"strings"
)

func TestGetCacheURL(t *testing.T) {
	viper.SetEnvPrefix("AEROFLARE")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindEnv("cache", "AEROFLARE_CACHE")

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
			t.Setenv("AEROFLARE_CACHE_URL", tt.cacheUrl)
			t.Setenv("AEROFLARE_CACHE", tt.cache)

			// Clear viper cache since we are not using Reset()
			viper.Set("cache-url", nil)
			viper.Set("cache", nil)

			result := GetCacheURL()
			if result != tt.expected {
				t.Errorf("GetCacheURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetGithubToken(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		ghToken     string
		expected    string
	}{
		{
			name:        "GITHUB_TOKEN takes precedence over GH_TOKEN",
			githubToken: "token1",
			ghToken:     "token2",
			expected:    "token1",
		},
		{
			name:        "GH_TOKEN used when GITHUB_TOKEN is empty",
			githubToken: "",
			ghToken:     "token2",
			expected:    "token2",
		},
		{
			name:        "Empty when both are empty",
			githubToken: "",
			ghToken:     "",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", tt.githubToken)
			t.Setenv("GH_TOKEN", tt.ghToken)
			
			// We assume the secret manager does not have github-token set in the test environment,
			// or if it does, we temporarily clear it out.
			// Currently testing the environment variable fallbacks.
			result := getGithubToken()
			
			// If secret manager has it set, result might not match tt.expected.
			// Ideally we would mock the manager, but the function instantiates it directly.
			// So we check if it matches the expected fallback when not found in manager.
			if result != tt.expected {
				// If a real token is returned from the secret manager, it will override our env vars.
				// We print a log but do not strictly fail if it looks like a real token from SM, 
				// though for a pure unit test we expect it to match if SM is empty.
				// For the purpose of the test, we assume SM is empty.
				t.Errorf("getGithubToken() = %v, want %v", result, tt.expected)
			}
		})
	}
}
