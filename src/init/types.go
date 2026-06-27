package setup

import (
	"fmt"
	"os"
	"strings"
)

// BackendType represents the index storage backend.
type BackendType string

const (
	BackendR2     BackendType = "r2"
	BackendNative BackendType = "native"
	BackendOCI    BackendType = "oci"
)

// String returns a human-readable label.
func (b BackendType) String() string {
	switch b {
	case BackendR2:
		return "Cloudflare R2"
	case BackendNative:
		return "Native OCI Tags"
	case BackendOCI:
		return "JSON index stored in OCI"
	default:
		return string(b)
	}
}

// GitProvider represents the Git hosting provider for CI/CD integration.
type GitProvider string

const (
	GitNone   GitProvider = "none"
	GitGitHub GitProvider = "github"
	GitGitLab GitProvider = "gitlab"
)

// String returns a human-readable label.
func (g GitProvider) String() string {
	switch g {
	case GitNone:
		return "None"
	case GitGitHub:
		return "GitHub"
	case GitGitLab:
		return "GitLab"
	default:
		return string(g)
	}
}

// InitConfig holds all parameters collected by the setup wizard.
type InitConfig struct {
	CacheName  string
	Registry   string
	Repository string

	Backend     BackendType
	GitProvider GitProvider

	CloudflareAccountID string
	CloudflareToken     string

	GitToken   string
	GitUsername string

	WorkerName string
	R2Bucket   string

	// Internal fields populated during provisioning.
	OCIToken  string
	ScriptTag string // Worker script tag from CF deployment, used for Workers Builds.
	CfTokenID string // Cloudflare API token ID, used for Workers Builds.
	GitCloneURL string // URL for pushing to the created git repository.
}

// DeriveDefaults populates computed fields from user-supplied values.
func (c *InitConfig) DeriveDefaults() {
	c.CacheName = strings.ToLower(c.CacheName)
	c.Repository = fmt.Sprintf("%s/nix-cache", c.CacheName)

	sanitized := strings.ReplaceAll(c.CacheName, "/", "-")
	c.WorkerName = fmt.Sprintf("aeroflare-%s", sanitized)

	if c.Backend == BackendR2 && c.R2Bucket == "" {
		c.R2Bucket = fmt.Sprintf("%s-index", sanitized)
	}
}

// Print helpers for consistent terminal output.

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "  \u2717 %s\n", msg)
}

func printSuccess(msg string) {
	fmt.Printf("  \u2713 %s\n", msg)
}

func printInfo(msg string) {
	fmt.Printf("  \u2022 %s\n", msg)
}

func printStep(step, total int, msg string) {
	fmt.Printf("\n  [%d/%d] %s\n", step, total, msg)
}

func printWarning(msg string) {
	fmt.Printf("  \u26a0 %s\n", msg)
}
