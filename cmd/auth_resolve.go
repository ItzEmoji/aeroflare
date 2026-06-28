package cmd

import (
	"fmt"
	"os"

	"aeroflare/src/secrets"
)

func isTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func GetGithubToken() string {
	if globalGithubToken != "" {
		return globalGithubToken
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return token
	}
	
	manager := getSecretsManager()
	if token, err := manager.Get("github-token"); err == nil && token != "" {
		return token
	}
	return ""
}

func RequireGithubToken() string {
	token := GetGithubToken()
	if token != "" {
		return token
	}
	
	if isTerminal() {
		fmt.Println("GitHub token is required but not found. Launching authentication...")
		return runInteractiveGithubAuth()
	}
	
	PrintError("GitHub token required. Please set GITHUB_TOKEN or run 'aeroflare auth login'.")
	os.Exit(1)
	return ""
}

func RequireGitlabToken() string {
	if globalGitlabToken != "" {
		return globalGitlabToken
	}
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		return token
	}
	
	manager := getSecretsManager()
	if token, err := manager.Get("gitlab-token"); err == nil && token != "" {
		return token
	} else if err != nil && err != secrets.ErrNotFound {
		fmt.Fprintf(os.Stderr, "Warning: failed to read from keychain: %v\n", err)
	}
	
	if isTerminal() {
		fmt.Println("GitLab token is required but not found. Launching authentication...")
		return runInteractiveGitlabAuth()
	}
	
	PrintError("GitLab token required. Please set GITLAB_TOKEN or run 'aeroflare auth login'.")
	os.Exit(1)
	return ""
}

func RequireCloudflareToken() (string, string) {
	manager := getSecretsManager()

	apiToken := globalCfToken
	if apiToken == "" {
		if t := os.Getenv("CLOUDFLARE_API_TOKEN"); t != "" {
			apiToken = t
		} else {
			if t, err := manager.Get("cf-token"); err == nil && t != "" {
				apiToken = t
			} else if err != nil && err != secrets.ErrNotFound {
				fmt.Fprintf(os.Stderr, "Warning: failed to read from keychain: %v\n", err)
			}
		}
	}
	
	userID := globalCfUserID
	if userID == "" {
		if u := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); u != "" {
			userID = u
		} else {
			if u, err := manager.Get("cf-user-id"); err == nil && u != "" {
				userID = u
			} else if err != nil && err != secrets.ErrNotFound {
				fmt.Fprintf(os.Stderr, "Warning: failed to read from keychain: %v\n", err)
			}
		}
	}
	
	if apiToken != "" && userID != "" {
		return apiToken, userID
	}
	
	if isTerminal() {
		fmt.Println("Cloudflare credentials required but incomplete. Launching authentication...")
		return runInteractiveCloudflareAuth()
	}
	
	PrintError("Cloudflare credentials required. Please set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID, or run 'aeroflare auth login'.")
	os.Exit(1)
	return "", ""
}

func GetOCIToken(registry string) (string, string) {
	manager := getSecretsManager()
	user, _ := manager.Get(fmt.Sprintf("oci-%s-username", registry))
	pass, _ := manager.Get(fmt.Sprintf("oci-%s-token", registry))
	return user, pass
}

func RequireOCIToken(registry string) (string, string) {
	user, pass := GetOCIToken(registry)
	if user != "" && pass != "" {
		return user, pass
	}
	
	if isTerminal() {
		fmt.Printf("Credentials for registry %s are required. Launching authentication...\n", registry)
		return runInteractiveOCIAuth(registry)
	}
	
	PrintError(fmt.Sprintf("Credentials required for registry %s. Run 'aeroflare auth login' to set them.", registry))
	os.Exit(1)
	return "", ""
}

func getTokenForRegistry(registry string) string {
	if registry == "ghcr.io" {
		token := RequireGithubToken()
		os.Setenv("oci_token", token)
		os.Setenv("GITHUB_TOKEN", token)
		return token
	} else if registry != "" {
		_, token := RequireOCIToken(registry)
		os.Setenv("oci_token", token)
		return token
	}
	return ""
}

func getOptionalTokenForRegistry(registry string) string {
	if registry == "ghcr.io" {
		token := GetGithubToken()
		if token != "" {
			os.Setenv("oci_token", token)
			os.Setenv("GITHUB_TOKEN", token)
		}
		return token
	} else if registry != "" {
		_, token := GetOCIToken(registry)
		if token != "" {
			os.Setenv("oci_token", token)
		}
		return token
	}
	return ""
}
