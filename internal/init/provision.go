package setup

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"net/http"
	"os"
	"os/exec"
	"strings"
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
	repoName := fmt.Sprintf("%s-proxy", strings.ReplaceAll(cfg.CacheName, "/", "-"))

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
	printInfo(fmt.Sprintf("Repository URL: %s", sanitizeCloneURL(cloneURL)))
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

// pushToGitRepo initializes a local git repository, creates necessary files
// (worker.js, wrangler.toml, .github/workflows/deploy.yml) and pushes to the remote.
func pushToGitRepo(cfg *InitConfig) error {
	if cfg.GitCloneURL == "" {
		return nil // No git repository created
	}

	tmpDir, err := os.MkdirTemp("", "aeroflare-push-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Fetch worker script. (pushToGitRepo always fetches the released script
	// rather than reusing a local override.)
	scriptPath, err := fetchLatestWorkerScript()
	if err == nil {
		scriptContent, _ := os.ReadFile(scriptPath)
		_ = os.WriteFile(tmpDir+"/worker.js", scriptContent, 0644)
	}

	// Write wrangler.toml
	wranglerToml := fmt.Sprintf(`name = "%s"
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

	_ = os.WriteFile(tmpDir+"/wrangler.toml", []byte(wranglerToml), 0644)

	// Write GitHub Actions workflow
	if cfg.GitProvider == GitGitHub {
		_ = os.MkdirAll(tmpDir+"/.github/workflows", 0755)
		workflow := `name: Deploy Worker
on:
  push:
    branches:
      - main
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
`
		_ = os.WriteFile(tmpDir+"/.github/workflows/deploy.yml", []byte(workflow), 0644)
	}

	// Git init, commit and push
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Aeroflare Setup"},
		{"git", "config", "user.email", "setup@aeroflare.dev"},
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit from Aeroflare Setup"},
		{"git", "branch", "-M", "main"},
		{"git", "remote", "add", "origin", cfg.GitCloneURL},
		{"git", "push", "-u", "origin", "main"},
	}

	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s failed: %w\nOutput: %s", c[1], err, strings.TrimSpace(string(out)))
		}
	}

	if cfg.GitProvider == GitGitHub {
		repoName := fmt.Sprintf("%s-proxy", strings.ReplaceAll(cfg.CacheName, "/", "-"))

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

// fetchLatestWorkerScript downloads the latest release tarball, extracts the
// worker script to a temp directory, and returns its path.
func fetchLatestWorkerScript() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases")
	if err != nil {
		return "", fmt.Errorf("fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decode releases: %w", err)
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("no releases found")
	}

	tag := releases[0].TagName
	printInfo(fmt.Sprintf("Using release %s", tag))

	tmpDir, err := os.MkdirTemp("", "aeroflare-worker-*")
	if err != nil {
		return "", err
	}

	tarURL := fmt.Sprintf("https://github.com/ItzEmoji/aeroflare/archive/refs/tags/%s.tar.gz", tag)
	cmd := exec.Command("sh", "-c", fmt.Sprintf("wget -qO- %s | tar -xz -C %s --strip-components=1", tarURL, tmpDir))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download release: %w", err)
	}

	scriptPath := fmt.Sprintf("%s/proxy/no-webui-native/worker.js", tmpDir)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("worker.js not found in release at %s", scriptPath)
	}

	return scriptPath, nil
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

// sanitizeCloneURL strips the "user:token@" credentials that
// createGitHubRepo/createGitLabRepo embed in the clone URL, so the token is
// never printed to the terminal.
func sanitizeCloneURL(cloneURL string) string {
	if idx := strings.Index(cloneURL, "@"); idx != -1 {
		prefix := cloneURL[:strings.Index(cloneURL, "//")+2]
		suffix := cloneURL[idx+1:]
		return prefix + suffix
	}
	return cloneURL
}
