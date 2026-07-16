package setup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/oci"
)

// RunProvision executes the infrastructure provisioning pipeline.
// Each step is idempotent where possible.
func RunProvision(cfg *InitConfig) error {
	totalSteps := countSteps(cfg)
	step := 0

	next := func(msg string) {
		step++
		printStep(step, totalSteps, msg)
	}

	next("Creating OCI repository...")
	if err := createOCIRepository(cfg); err != nil {
		return fmt.Errorf("create OCI repository: %w", err)
	}
	printSuccess("OCI repository created")

	next("Checking repository visibility...")
	if err := checkRepositoryVisibility(cfg); err != nil {
		printWarning(fmt.Sprintf("%v", err))
	}

	if cfg.GitProvider != GitNone {
		next("Creating Git repository...")
		if err := createGitRepository(cfg); err != nil {
			return fmt.Errorf("create git repository: %w", err)
		}
		printSuccess(fmt.Sprintf("%s repository created", cfg.GitProvider))
	}

	next("Deploying Cloudflare Worker...")
	if err := deployWorker(cfg); err != nil {
		return fmt.Errorf("deploy worker: %w", err)
	}
	printSuccess("Worker deployed")

	next("Configuring Worker...")
	if err := configureWorker(cfg); err != nil {
		return fmt.Errorf("configure worker: %w", err)
	}
	printSuccess("Worker configured")

	if cfg.GitProvider != GitNone {
		next("Pushing code to Git repository...")
		if err := pushToGitRepo(cfg); err != nil {
			printWarning(fmt.Sprintf("Could not push to git repository: %v", err))
		} else {
			printSuccess("Code pushed to repository successfully")
		}
	}

	fmt.Println()
	printSuccess("Setup complete! Your Aeroflare cache is ready.")

	// Show the worker URL if available.
	if subdomain := getWorkersSubdomain(cfg.CloudflareAccountID, cfg.CloudflareToken); subdomain != "" {
		printInfo(fmt.Sprintf("Worker URL: https://%s.%s.workers.dev", cfg.WorkerName, subdomain))
	}

	return nil
}

// countSteps returns the total number of provisioning steps for progress display.
func countSteps(cfg *InitConfig) int {
	n := 4 // OCI repo + visibility + deploy + configure
	if cfg.GitProvider != GitNone {
		n += 2 // create repo + connect builds
	}
	return n
}

// createOCIRepository pushes an initial config manifest, which auto-creates
// the package on registries like ghcr.io.
func createOCIRepository(cfg *InitConfig) error {
	if cfg.Registry == "registry.gitlab.com" && cfg.GitProvider == GitGitLab {
		if err := ensureGitLabProjectExists(cfg.GitToken, cfg.CacheName); err != nil {
			return fmt.Errorf("ensure GitLab project exists: %w", err)
		}
	}

	// When the registry is the provider's own container registry (ghcr.io
	// for GitHub, registry.gitlab.com for GitLab), the git token we already
	// collected also works as the OCI token, so pass it through explicitly
	// instead of making cmdutil.RegistryAuth look for a separate credential.
	var explicitToken string
	if (cfg.Registry == "ghcr.io" && cfg.GitProvider == GitGitHub) ||
		(cfg.Registry == "registry.gitlab.com" && cfg.GitProvider == GitGitLab) {
		explicitToken = cfg.GitToken
	}

	auth := cmdutil.RegistryAuth(cfg.Registry, explicitToken)
	if auth == nil {
		return fmt.Errorf("no OCI authentication token found \u2014 configure your environment or secrets manager")
	}
	cfg.OCIToken = explicitToken

	return oci.PushConfigManifest(cfg.Registry, cfg.Repository, auth, map[string]string{})
}

// checkRepositoryVisibility reminds the user to set package visibility.
// There is no API to do this automatically for ghcr.io, so it always
// returns nil; the caller (RunProvision) only prints the message as a
// warning, never treats it as a failure.
func checkRepositoryVisibility(cfg *InitConfig) error {
	if cfg.Registry != "ghcr.io" {
		printInfo("Non-ghcr.io registry \u2014 set package visibility to public manually if needed.")
		return nil
	}

	parts := strings.SplitN(cfg.Repository, "/", 2)
	owner := ""
	if len(parts) >= 1 {
		owner = parts[0]
	}

	printInfo(fmt.Sprintf("Note: GitHub requires package visibility to be set to public manually at https://github.com/%s?tab=packages", owner))
	return nil
}

// createGitRepository creates a remote Git repository on the selected provider.
func createGitRepository(cfg *InitConfig) error {
	repoName := proxyRepoName(cfg.CacheName)

	var cloneURL string
	var err error

	switch cfg.GitProvider {
	case GitGitHub:
		cloneURL, err = createGitHubRepo(cfg.GitToken, repoName)
	case GitGitLab:
		cloneURL, err = createGitLabRepo(cfg.GitToken, repoName)
	default:
		return nil
	}

	if err != nil {
		return err
	}

	cfg.GitCloneURL = cloneURL
	printInfo(fmt.Sprintf("Repository URL: %s", cloneURL))
	return nil
}

// deployWorker fetches the latest worker script and deploys it to Cloudflare.
func deployWorker(cfg *InitConfig) error {
	scriptPath, err := resolveWorkerScript(cfg)
	if err != nil {
		return err
	}

	vars := workerEnvVars(cfg)

	// Optional registry PAT, stored as an encrypted secret binding so the Worker
	// can authenticate to the registry (skips GHCR's token exchange; enables
	// private repos). The Worker uses NIXCACHE_TOKEN verbatim as the bearer
	// token, so we base64-encode the raw PAT here — that's exactly the value
	// GHCR expects. Omitted entirely when the user didn't provide a token.
	secrets := map[string]string{}
	if cfg.WorkerToken != "" {
		secrets["NIXCACHE_TOKEN"] = base64.StdEncoding.EncodeToString([]byte(cfg.WorkerToken))
	}

	scriptTag, err := deployWorkerViaAPI(
		cfg.CloudflareAccountID, cfg.CloudflareToken,
		cfg.WorkerName, scriptPath, "2024-12-01", vars, secrets,
	)
	if err != nil {
		return err
	}

	cfg.ScriptTag = scriptTag
	return nil
}

// configureWorker performs post-deployment configuration (workers.dev route).
func configureWorker(cfg *InitConfig) error {
	if err := enableWorkerRoute(cfg.CloudflareAccountID, cfg.CloudflareToken, cfg.WorkerName); err != nil {
		printWarning(fmt.Sprintf("Could not enable workers.dev route: %v", err))
	}
	return nil
}

// proxyRepoBranch is the branch the generated proxy repository is created with,
// and the one its deploy workflow triggers on.
const proxyRepoBranch = "main"

// pushToGitRepo fills the freshly created proxy repository with its initial
// commit (worker.js, wrangler.toml, and for GitHub a deploy workflow).
//
// The files are committed through the provider's API rather than by shelling
// out to `git`. init's whole job is to set a machine up from nothing, so
// depending on git being installed defeated the point — and the API needs no
// working tree, no temp directory, and no credentials in a remote URL.
func pushToGitRepo(cfg *InitConfig) error {
	if cfg.GitCloneURL == "" {
		return nil // No git repository created
	}

	files := proxyRepoFiles(cfg)
	repoName := proxyRepoName(cfg.CacheName)
	const message = "Initial commit from Aeroflare Setup"

	switch cfg.GitProvider {
	case GitGitHub:
		if err := commitFilesToGitHub(cfg.GitToken, cfg.GitUsername, repoName, proxyRepoBranch, message, files); err != nil {
			return err
		}
	case GitGitLab:
		project := fmt.Sprintf("%s/%s", cfg.GitUsername, repoName)
		if err := commitFilesToGitLab(cfg.GitToken, project, proxyRepoBranch, message, files); err != nil {
			return err
		}
	}

	if cfg.GitProvider == GitGitHub {
		printInfo("Configuring GitHub Actions secrets...")
		err1 := setGitHubSecret(cfg.GitToken, cfg.GitUsername, repoName, "CLOUDFLARE_API_TOKEN", cfg.CloudflareToken)
		err2 := setGitHubSecret(cfg.GitToken, cfg.GitUsername, repoName, "CLOUDFLARE_ACCOUNT_ID", cfg.CloudflareAccountID)

		if err1 != nil || err2 != nil {
			printWarning("Failed to set secrets automatically.")
			printInfo("Please add CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID as repository secrets on GitHub")
			printInfo(fmt.Sprintf("Settings URL: https://github.com/%s/%s/settings/secrets/actions", cfg.GitUsername, repoName))
		} else {
			printSuccess("GitHub Actions secrets configured successfully")
		}
	}

	return nil
}

// proxyRepoName is the repository name generated for a cache. Worker and repo
// names can't contain "/", so a namespaced cache name is flattened.
func proxyRepoName(cacheName string) string {
	return fmt.Sprintf("%s-proxy", strings.ReplaceAll(cacheName, "/", "-"))
}

// proxyRepoFiles builds the contents of the generated proxy repository, keyed
// by path within it. The worker script is fetched from the latest release
// (never a local override, so the pushed repo matches a published version); if
// that fetch fails the remaining files are still worth committing, so the
// script is simply omitted rather than failing the whole step.
func proxyRepoFiles(cfg *InitConfig) map[string]string {
	files := map[string]string{
		"wrangler.toml": wranglerTOML(cfg),
	}

	if scriptPath, err := fetchLatestWorkerScript(); err == nil {
		if script, err := os.ReadFile(scriptPath); err == nil {
			files["worker.js"] = string(script)
		}
	} else {
		printWarning(fmt.Sprintf("Could not fetch worker.js for the repository: %v", err))
	}

	if cfg.GitProvider == GitGitHub {
		files[".github/workflows/deploy.yml"] = deployWorkflow()
	}

	return files
}

// wranglerTOML renders the wrangler config for the generated repository.
func wranglerTOML(cfg *InitConfig) string {
	return fmt.Sprintf(`name = "%s"
main = "worker.js"
compatibility_date = "2024-12-01"

[vars]
# Repository path, exactly as it lives in the registry.
NIXCACHE_REPO = "%s"
# Registry base URL WITH scheme but WITHOUT /v2 (the worker adds the spec's /v2).
NIXCACHE_REGISTRY_URL = "%s"

# Optional: a registry bearer token, set as a secret (NOT a plaintext var). The
# Worker uses it verbatim as the bearer, so for GHCR it must be the BASE64 PAT:
#   printf '%%s' "github_pat_xxx" | base64 | wrangler secret put NIXCACHE_TOKEN
# GHCR accepts it directly, skipping the token exchange (faster, private repos).
`, cfg.WorkerName, cfg.Repository, workerRegistryURL(cfg.Registry))
}

// deployWorkflow renders the GitHub Actions workflow that redeploys the Worker
// on every push to the default branch.
func deployWorkflow() string {
	return fmt.Sprintf(`name: Deploy Worker
on:
  push:
    branches:
      - %s
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Deploy
        uses: cloudflare/wrangler-action@v3
        with:
          apiToken: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          accountId: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
`, proxyRepoBranch)
}

// resolveWorkerScript finds a worker.js to deploy.
// Checks local conventional paths first, then fetches from the latest release.
func resolveWorkerScript(cfg *InitConfig) (string, error) {
	localPaths := []string{
		"./proxy/no-webui-native/worker.js",
		"./aeroflare-proxy/proxy/no-webui-native/worker.js",
		"./worker.js",
	}
	for _, p := range localPaths {
		if _, err := os.Stat(p); err == nil {
			printInfo(fmt.Sprintf("Using local worker script: %s", p))
			return p, nil
		}
	}

	printInfo("No local worker.js found, fetching latest release...")
	return fetchLatestWorkerScript()
}

// workerScriptRelPath is where the worker script lives inside the release
// tarball, below the single top-level directory GitHub wraps an archive in.
const workerScriptRelPath = "proxy/no-webui-native/worker.js"

// maxWorkerScriptBytes bounds the extracted script. It is a few hundred KB in
// practice; the limit just stops a malformed archive from exhausting memory.
const maxWorkerScriptBytes = 16 << 20

// latestReleaseTag returns the tag of the most recent published release.
func latestReleaseTag() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases/latest")
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch latest release: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest release has no tag")
	}
	return release.TagName, nil
}

// fetchLatestWorkerScript downloads the latest release tarball, extracts the
// worker script to a temp directory, and returns its path. The tarball is read
// in-process rather than through `wget … | tar`, so init works on a machine
// that has neither installed.
func fetchLatestWorkerScript() (string, error) {
	tag, err := latestReleaseTag()
	if err != nil {
		return "", err
	}
	printInfo(fmt.Sprintf("Using release %s", tag))

	tarURL := fmt.Sprintf("https://github.com/ItzEmoji/aeroflare/archive/refs/tags/%s.tar.gz", tag)
	resp, err := http.Get(tarURL)
	if err != nil {
		return "", fmt.Errorf("download release %s: %w", tag, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release %s: HTTP %d", tag, resp.StatusCode)
	}

	script, err := workerScriptFromTarball(resp.Body)
	if err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp("", "aeroflare-worker-*")
	if err != nil {
		return "", err
	}

	scriptPath := filepath.Join(tmpDir, "worker.js")
	if err := os.WriteFile(scriptPath, script, 0644); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	return scriptPath, nil
}

// workerScriptFromTarball reads a gzipped release tarball and returns the
// contents of the worker script. Entries are matched on their path below the
// archive's top-level directory (named "<repo>-<tag>"), which is the component
// `tar --strip-components=1` used to discard. Nothing is written using a path
// taken from the archive, so a hostile entry name cannot escape anywhere.
func workerScriptFromTarball(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("read release tarball: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if _, rest, ok := strings.Cut(hdr.Name, "/"); ok && rest == workerScriptRelPath {
			return io.ReadAll(io.LimitReader(tr, maxWorkerScriptBytes))
		}
	}

	return nil, fmt.Errorf("%s not found in release tarball", workerScriptRelPath)
}

// workerEnvVars builds the environment variable map for the worker deployment.
func workerEnvVars(cfg *InitConfig) map[string]string {
	return map[string]string{
		"NIXCACHE_REPO":         cfg.Repository,
		"NIXCACHE_REGISTRY_URL": workerRegistryURL(cfg.Registry),
	}
}

// workerRegistryURL turns a registry host (e.g. "ghcr.io") into the base URL the
// worker expects for NIXCACHE_REGISTRY_URL (e.g. "https://ghcr.io"). The worker
// appends the spec's "/v2" API prefix itself, so it must not be included here.
func workerRegistryURL(registry string) string {
	if strings.HasPrefix(registry, "http://") || strings.HasPrefix(registry, "https://") {
		return strings.TrimRight(registry, "/")
	}
	return "https://" + strings.TrimRight(registry, "/")
}
