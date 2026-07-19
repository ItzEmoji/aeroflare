package setup

import (
	"fmt"
	"strings"
)

// InitConfig holds all parameters collected by the setup wizard.
type InitConfig struct {
	CacheName  string
	Registry   string
	Repository string

	CloudflareAccountID string
	CloudflareToken     string

	WorkerName string

	// WorkerToken is an optional registry PAT stored on the Worker as the
	// NIXCACHE_TOKEN secret. When set, the Worker uses it directly as the GHCR
	// bearer token (skipping the token exchange: faster, fewer requests) and can
	// reach private repositories. Empty means the Worker authenticates
	// anonymously, which only works for public caches. On the ghcr.io path it is
	// collected from a dedicated prompt (see promptWorkerToken), kept separate
	// from OCIToken so the broad push credential is never embedded in the Worker.
	WorkerToken string

	// OCIToken is the registry credential resolved for Registry, used to push
	// the cache's config manifest. It is deliberately not reused as the
	// WorkerToken (see promptWorkerToken).
	OCIToken string

	// ScriptTag is the Worker script tag returned by the Cloudflare deploy API;
	// reserved for a future Workers Builds integration, not yet read elsewhere.
	ScriptTag string
}

// DeriveDefaults populates computed fields (Repository, WorkerName) from the
// cache name and registry the user already supplied. Must be called after
// CacheName/Registry are set and before the values are used elsewhere (e.g.
// by the credentials prompt or provisioning).
func (c *InitConfig) DeriveDefaults() {
	c.CacheName = strings.ToLower(c.CacheName)

	// The repository is the cache name exactly as it lives in the registry.
	c.Repository = c.CacheName

	// Worker names can't contain "/", so slashes in the cache name (e.g.
	// "user/repo") are flattened to hyphens.
	sanitized := strings.ReplaceAll(c.CacheName, "/", "-")
	c.WorkerName = fmt.Sprintf("aeroflare-%s", sanitized)
}

// Print helpers below give the setup wizard and provisioning pipeline a
// consistent terminal output style (icon + indented message).

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
