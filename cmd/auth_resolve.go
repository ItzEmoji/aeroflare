package cmd

import (
	"errors"
	"fmt"
	"os"

	"aeroflare/src/auth"
)

func isTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func RequireGithubToken() string {
	if globalGithubToken != "" {
		return globalGithubToken
	}
	
	token, err := auth.ResolveGithubToken(getSecretsManager())
	if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
		fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", err)
	}
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
	
	token, err := auth.ResolveGitlabToken(getSecretsManager())
	if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
		fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", err)
	}
	if token != "" {
		return token
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
	apiToken := globalCfToken
	if apiToken == "" {
		var err error
		apiToken, err = auth.NewResolver("cf-token").
			WithEnv("CLOUDFLARE_API_TOKEN").
			WithSecretsManager(getSecretsManager()).
			Resolve()
		if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
			fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", err)
		}
	}
	
	userID := globalCfUserID
	if userID == "" {
		var err error
		userID, err = auth.NewResolver("cf-user-id").
			WithEnv("CLOUDFLARE_ACCOUNT_ID").
			WithSecretsManager(getSecretsManager()).
			Resolve()
		if err != nil && !errors.Is(err, auth.ErrTokenNotFound) {
			fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", err)
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
	user, errUser := auth.NewResolver(fmt.Sprintf("oci-%s-username", registry)).
		WithSecretsManager(getSecretsManager()).
		Resolve()
	if errUser != nil && !errors.Is(errUser, auth.ErrTokenNotFound) {
		fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", errUser)
	}
	pass, errPass := auth.NewResolver(fmt.Sprintf("oci-%s-token", registry)).
		WithSecretsManager(getSecretsManager()).
		Resolve()
	if errPass != nil && !errors.Is(errPass, auth.ErrTokenNotFound) {
		fmt.Fprintf(os.Stderr, "Warning: failed to read token from keychain: %v\n", errPass)
	}
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
	if registry == "" {
		return ""
	}
	token, _ := auth.ResolveRegistryToken(registry, getSecretsManager())
	if token != "" {
		os.Setenv("oci_token", token)
		if registry == "ghcr.io" {
			os.Setenv("GITHUB_TOKEN", token)
		}
	}
	return token
}
