package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	network "aeroflare/src"
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

	if cfg.Backend == BackendR2 {
		next("Creating R2 bucket...")
		if err := createR2Bucket(cfg); err != nil {
			return fmt.Errorf("create R2 bucket: %w", err)
		}
		printSuccess(fmt.Sprintf("R2 bucket '%s' is ready", cfg.R2Bucket))
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
	if cfg.Backend == BackendR2 {
		n++ // create R2 bucket
	}
	return n
}

// createOCIRepository pushes an initial config manifest, which auto-creates
// the package on registries like ghcr.io.
func createOCIRepository(cfg *InitConfig) error {
	// network.GetToken relies on GITHUB_TOKEN/GH_TOKEN/GITLAB_TOKEN env vars.
	// We inject the token we just obtained from the wizard.
	if cfg.GitToken != "" {
		os.Setenv("GITHUB_TOKEN", cfg.GitToken)
		os.Setenv("GITLAB_TOKEN", cfg.GitToken)
		if cfg.GitUsername != "" {
			os.Setenv("AEROFLARE_GIT_USERNAME", cfg.GitUsername)
		}
	}

	if cfg.Registry == "registry.gitlab.com" && cfg.GitProvider == GitGitLab {
		if err := ensureGitLabProjectExists(cfg.GitToken, cfg.CacheName); err != nil {
			return fmt.Errorf("ensure GitLab project exists: %w", err)
		}
	}

	ociToken := network.GetToken(cfg.Registry, cfg.Repository)
	if ociToken == "" {
		return fmt.Errorf("no OCI authentication token found \u2014 set GITHUB_TOKEN or GH_TOKEN")
	}
	cfg.OCIToken = ociToken

	annotations := map[string]string{
		"aeroflare.backend": string(cfg.Backend),
	}
	if cfg.Backend == BackendR2 && cfg.R2Bucket != "" {
		annotations["aeroflare.r2.bucket"] = cfg.R2Bucket
	}

	return network.PushConfigManifest(cfg.Registry, cfg.Repository, ociToken, annotations)
}

// checkRepositoryVisibility reminds the user to set package visibility.
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

// createR2Bucket creates the Cloudflare R2 bucket.
func createR2Bucket(cfg *InitConfig) error {
	return createR2BucketViaAPI(cfg.CloudflareAccountID, cfg.CloudflareToken, cfg.R2Bucket)
}

// deployWorker fetches the latest worker script and deploys it to Cloudflare.
func deployWorker(cfg *InitConfig) error {
	scriptPath, err := resolveWorkerScript(cfg)
	if err != nil {
		return err
	}

	vars := workerEnvVars(cfg)
	r2Bucket := ""
	if cfg.Backend == BackendR2 {
		r2Bucket = cfg.R2Bucket
	}

	scriptTag, err := deployWorkerViaAPI(
		cfg.CloudflareAccountID, cfg.CloudflareToken,
		cfg.WorkerName, scriptPath, "2024-12-01", vars, r2Bucket,
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
	defer os.RemoveAll(tmpDir)

	// Fetch worker script
	backendSuffix := "json"
	if cfg.Backend == BackendR2 {
		backendSuffix = "r2"
	} else if cfg.Backend == BackendNative {
		backendSuffix = "native"
	}
	scriptPath, err := fetchLatestWorkerScript(backendSuffix)
	if err == nil {
		scriptContent, _ := os.ReadFile(scriptPath)
		os.WriteFile(tmpDir+"/worker.js", scriptContent, 0644)
	}

	// Write wrangler.toml
	repo := strings.TrimSuffix(cfg.Repository, "/nix-cache")
	wranglerToml := fmt.Sprintf(`name = "%s"
main = "worker.js"
compatibility_date = "2024-12-01"

[vars]
NIXCACHE_REPO = "%s"
NIXCACHE_REGISTRY = "%s"
NIXCACHE_UPSTREAM = "https://cache.nixos.org"
NIXCACHE_INDEX_TTL = "200"
`, cfg.WorkerName, repo, cfg.Registry)

	if cfg.Backend == BackendR2 {
		wranglerToml += fmt.Sprintf(`
[[r2_buckets]]
binding = "BUCKET"
bucket_name = "%s"
`, cfg.R2Bucket)
	}
	os.WriteFile(tmpDir+"/wrangler.toml", []byte(wranglerToml), 0644)

	// Write GitHub Actions workflow
	if cfg.GitProvider == GitGitHub {
		os.MkdirAll(tmpDir+"/.github/workflows", 0755)
		workflow := fmt.Sprintf(`name: Deploy Worker
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
`)
		os.WriteFile(tmpDir+"/.github/workflows/deploy.yml", []byte(workflow), 0644)
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
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git %s failed: %w", c[1], err)
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
	backendSuffix := "json"
	if cfg.Backend == BackendR2 {
		backendSuffix = "r2"
	} else if cfg.Backend == BackendNative {
		backendSuffix = "native"
	}

	localPaths := []string{
		fmt.Sprintf("./proxy/no-webui-%s/worker.js", backendSuffix),
		fmt.Sprintf("./aeroflare-proxy/proxy/no-webui-%s/worker.js", backendSuffix),
		"./worker.js",
	}
	for _, p := range localPaths {
		if _, err := os.Stat(p); err == nil {
			printInfo(fmt.Sprintf("Using local worker script: %s", p))
			return p, nil
		}
	}

	printInfo("No local worker.js found, fetching latest release...")
	return fetchLatestWorkerScript(backendSuffix)
}

// fetchLatestWorkerScript downloads the latest release tarball, extracts the
// worker script to a temp directory, and returns its path.
func fetchLatestWorkerScript(backendSuffix string) (string, error) {
	resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases")
	if err != nil {
		return "", fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

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
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download release: %w", err)
	}

	scriptPath := fmt.Sprintf("%s/proxy/no-webui-%s/worker.js", tmpDir, backendSuffix)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("worker.js not found in release at %s", scriptPath)
	}

	return scriptPath, nil
}

// workerEnvVars builds the environment variable map for the worker deployment.
func workerEnvVars(cfg *InitConfig) map[string]string {
	repo := strings.TrimSuffix(cfg.Repository, "/nix-cache")
	return map[string]string{
		"NIXCACHE_REPO":      repo,
		"NIXCACHE_REGISTRY":  cfg.Registry,
		"NIXCACHE_UPSTREAM":  "https://cache.nixos.org",
		"NIXCACHE_INDEX_TTL": "200",
	}
}

// sanitizeCloneURL removes embedded tokens from a clone URL for display.
func sanitizeCloneURL(url string) string {
	if idx := strings.Index(url, "@"); idx != -1 {
		prefix := url[:strings.Index(url, "//")+2]
		suffix := url[idx+1:]
		return prefix + suffix
	}
	return url
}
