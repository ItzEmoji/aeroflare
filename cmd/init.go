package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"strings"

	network "aeroflare/src"
	"aeroflare/src/proxy"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var initialIndexTypeFlag string

// deployWorkerViaAPI uploads a worker script to Cloudflare using the Workers API directly,
// eliminating the need for wrangler/npx as a runtime dependency.
func deployWorkerViaAPI(cfAccountId, cfApiToken, workerName, scriptPath, compatDate string, vars map[string]string, r2Bucket string) (string, error) {
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read worker script: %w", err)
	}

	// Build multipart body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Build metadata
	bindings := []map[string]interface{}{}
	for k, v := range vars {
		bindings = append(bindings, map[string]interface{}{
			"type": "plain_text",
			"name": k,
			"text": v,
		})
	}
	if r2Bucket != "" {
		bindings = append(bindings, map[string]interface{}{
			"type":        "r2_bucket",
			"name":        "BUCKET",
			"bucket_name": r2Bucket,
		})
	}

	metadata := map[string]interface{}{
		"main_module":        "worker.js",
		"compatibility_date": compatDate,
		"bindings":           bindings,
	}
	metadataJSON, _ := json.Marshal(metadata)

	// Add metadata part
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metaHeader.Set("Content-Type", "application/json")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return "", fmt.Errorf("failed to create metadata part: %w", err)
	}
	metaPart.Write(metadataJSON)

	// Add worker script part (must have filename for module workers)
	scriptHeader := make(textproto.MIMEHeader)
	scriptHeader.Set("Content-Disposition", `form-data; name="worker.js"; filename="worker.js"`)
	scriptHeader.Set("Content-Type", "application/javascript+module")
	scriptPart, err := writer.CreatePart(scriptHeader)
	if err != nil {
		return "", fmt.Errorf("failed to create script part: %w", err)
	}
	scriptPart.Write(scriptContent)

	writer.Close()

	req, err := http.NewRequest("PUT",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", cfAccountId, workerName),
		&body,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloudflare API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Extract script_tag from response for Workers Builds integration
	var deployResult struct {
		Result struct {
			Tag string `json:"tag"`
		} `json:"result"`
	}
	json.Unmarshal(respBody, &deployResult)

	return deployResult.Result.Tag, nil
}

// enableWorkerRoute assigns a workers.dev subdomain route so the worker is reachable.
func enableWorkerRoute(cfAccountId, cfApiToken, workerName string) error {
	payload := map[string]interface{}{
		"enabled": true,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s/subdomain", cfAccountId, workerName),
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Ignore errors here — the subdomain may already be enabled
	return nil
}

// getWorkersSubdomain fetches the workers.dev subdomain for the account.
func getWorkersSubdomain(cfAccountId, cfApiToken string) string {
	req, err := http.NewRequest("GET",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/subdomain", cfAccountId),
		nil,
	)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Subdomain string `json:"subdomain"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && result.Result.Subdomain != "" {
		return result.Result.Subdomain
	}
	return ""
}

// createR2BucketViaAPI creates an R2 bucket using the Cloudflare API directly.
func createR2BucketViaAPI(cfAccountId, cfApiToken, bucketName string) error {
	payload := map[string]string{"name": bucketName}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/r2/buckets", cfAccountId),
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		// 409 means bucket already exists — not a fatal error
		if resp.StatusCode == 409 {
			PrintInfo(fmt.Sprintf("R2 bucket '%s' already exists, continuing...", bucketName))
			return nil
		}
		return fmt.Errorf("cloudflare API returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// connectWorkerToGitBuilds uses the Cloudflare Workers Builds API to connect a Worker
// to a Git repository for automatic CI/CD deployments.
func connectWorkerToGitBuilds(cfAccountId, cfApiToken, cfTokenId, scriptTag, repoName, gitUsername, gitProvider string) error {
	// Step 1: Create or upsert the repo connection
	PrintInfo("Connecting repository to Cloudflare Workers Builds...")
	repoConnectionUUID, err := createRepoConnection(cfAccountId, cfApiToken, repoName, gitUsername, gitProvider)
	if err != nil {
		return fmt.Errorf("failed to create repo connection: %w", err)
	}
	PrintSuccess(fmt.Sprintf("Repository connection created: %s", repoConnectionUUID))

	// Step 2: Create a build token
	PrintInfo("Creating build token...")
	buildTokenUUID, err := createBuildToken(cfAccountId, cfApiToken, cfTokenId, repoName)
	if err != nil {
		return fmt.Errorf("failed to create build token: %w", err)
	}

	// Step 3: Create a trigger linking the worker to the repo
	PrintInfo("Creating build trigger...")
	err = createBuildTrigger(cfAccountId, cfApiToken, scriptTag, repoConnectionUUID, buildTokenUUID)
	if err != nil {
		return fmt.Errorf("failed to create build trigger: %w", err)
	}

	return nil
}

// createRepoConnection upserts a repository connection for Workers Builds CI/CD.
func createRepoConnection(cfAccountId, cfApiToken, repoName, gitUsername, gitProvider string) (string, error) {
	payload := map[string]string{
		"repo_id":               repoName,
		"repo_name":             repoName,
		"provider_type":         gitProvider,
		"provider_account_id":   gitUsername,
		"provider_account_name": gitUsername,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("PUT",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/repos/connections", cfAccountId),
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResponse struct {
			Errors []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(respBody, &errResponse) == nil && len(errResponse.Errors) > 0 {
			msg := errResponse.Errors[0].Message
			if errResponse.Errors[0].Code == 8000008 {
				msg = "Cloudflare is not authorized to access your Git repository. You must install the Cloudflare GitHub App."
			}
			return "", fmt.Errorf("%s", msg)
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			RepoConnectionUUID string `json:"repo_connection_uuid"`
		} `json:"result"`
	}
	json.Unmarshal(respBody, &result)

	return result.Result.RepoConnectionUUID, nil
}

// createBuildToken creates a build authentication token for Workers Builds.
func createBuildToken(cfAccountId, cfApiToken, cfTokenId, repoName string) (string, error) {
	payload := map[string]string{
		"build_token_name":   fmt.Sprintf("aeroflare-%s", repoName),
		"build_token_secret": cfApiToken,
		"cloudflare_token_id": cfTokenId,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/tokens", cfAccountId),
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			BuildTokenUUID string `json:"build_token_uuid"`
		} `json:"result"`
	}
	json.Unmarshal(respBody, &result)

	return result.Result.BuildTokenUUID, nil
}

// createBuildTrigger creates a CI/CD trigger linking a Worker to a repo connection.
func createBuildTrigger(cfAccountId, cfApiToken, scriptTag, repoConnectionUUID, buildTokenUUID string) error {
	payload := map[string]interface{}{
		"external_script_id":  scriptTag,
		"repo_connection_uuid": repoConnectionUUID,
		"build_token_uuid":    buildTokenUUID,
		"trigger_name":        "Production Deploy",
		"build_command":       "",
		"deploy_command":      "npx wrangler deploy",
		"root_directory":      "/",
		"branch_includes":     []string{"main"},
		"branch_excludes":     []string{},
		"path_includes":       []string{"*"},
		"path_excludes":       []string{},
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/triggers", cfAccountId),
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// getCfApiTokenId retrieves the token ID for the current API token using the /user/tokens/verify endpoint.
func getCfApiTokenId(cfApiToken string) string {
	req, err := http.NewRequest("GET", "https://api.cloudflare.com/client/v4/user/tokens/verify", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil {
		return result.Result.ID
	}
	return ""
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Aeroflare (quick setup)",
	Run: func(cmd *cobra.Command, args []string) {
		registry, repository := network.GetRegistryAndRepository()

		ociToken := network.GetToken(registry, repository)
		if ociToken == "" {
			PrintError("Authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
			os.Exit(1)
		}

		// Check if it has already been created
		tokenMgr := proxy.NewTokenManager(registry, repository, getGithubToken())
		remoteConf, existingAnnotations, _ := proxy.BootstrapConfigWithAnnotations(context.Background(), nil, registry, repository, tokenMgr)

		var indexType string
		var r2Bucket string

		isCreated := false
		if existingAnnotations != nil {
			if b := existingAnnotations["aeroflare.backend"]; b != "" {
				isCreated = true
				if b == "r2" {
					indexType = "r2"
				} else {
					indexType = "json"
				}
			}
			if b := existingAnnotations["aeroflare.r2.bucket"]; b != "" {
				r2Bucket = b
			}
		} else if remoteConf != nil && remoteConf.PublicKey != "" {
			isCreated = true
		}

		if isCreated {
			fmt.Println("❌ Aeroflare has already been initialized for this cache.")
			os.Exit(1)
		}

		if !isCreated {
			indexType = os.Getenv("AEROFLARE_INITIAL_INDEX_TYPE")
			if indexType == "" {
				indexType = initialIndexTypeFlag
			}

			if indexType == "" {
				// Interactive selection
				err := huh.NewForm(
					huh.NewGroup(
						huh.NewSelect[string]().
							Title("Choose your cache backend").
							Options(
								huh.NewOption("Cloudflare R2", "r2"),
								huh.NewOption("cache-index.json", "json"),
							).
							Value(&indexType),
					),
				).Run()
				if err != nil {
					os.Exit(0)
				}
			} else {
				if indexType != "r2" && indexType != "json" {
					indexType = "json"
				}
			}

			if indexType == "r2" {
				var publicKey string
				var r2PublicURL string
				var r2Endpoint string

				r2Form := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().Title("Public Key").Description("Enter your nix cache public key (optional)").Value(&publicKey),
						huh.NewInput().Title("R2 Bucket Name").Value(&r2Bucket),
						huh.NewInput().Title("R2 Public URL (e.g., https://pub-xxx.r2.dev)").Value(&r2PublicURL),
						huh.NewInput().Title("R2 S3 API Endpoint").Value(&r2Endpoint),
					),
				)
				err := r2Form.Run()
				if err != nil {
					os.Exit(0)
				}

				annotations := map[string]string{
					"aeroflare.backend":     "r2",
					"public-key":            publicKey,
					"aeroflare.r2.bucket":   r2Bucket,
					"public-r2-url":         r2PublicURL,
					"aeroflare.r2.endpoint": r2Endpoint,
				}

				PrintInfo("Saving configuration to OCI manifest annotations...")
				err = network.PushConfigManifest(registry, repository, ociToken, annotations)
				if err != nil {
					PrintError(fmt.Sprintf("Failed to save config: %v", err))
					os.Exit(1)
				}
				PrintSuccess("Configuration successfully saved to cache-config manifest!")
			} else {
				annotations := map[string]string{
					"aeroflare.backend": "cache-index",
				}
				PrintInfo("Saving configuration to OCI manifest annotations...")
				err := network.PushConfigManifest(registry, repository, ociToken, annotations)
				if err != nil {
					PrintError(fmt.Sprintf("Failed to save config: %v", err))
					os.Exit(1)
				}
				PrintSuccess("Configuration successfully saved to cache-config manifest!")
			}
		}

		// Fetch releases
		PrintInfo("Fetching available releases...")
		resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases")
		if err != nil {
			PrintError(fmt.Sprintf("Failed to fetch releases: %v", err))
			os.Exit(1)
		}
		defer resp.Body.Close()

		var releases []struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			PrintError(fmt.Sprintf("Failed to decode releases: %v", err))
			os.Exit(1)
		}

		if len(releases) == 0 {
			PrintError("No releases found.")
			os.Exit(1)
		}

		var options []huh.Option[string]
		for _, r := range releases {
			options = append(options, huh.NewOption(r.TagName, r.TagName))
		}

		var releaseTag string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which release of aeroflare do you want to use?").
					Options(options...).
					Value(&releaseTag),
			),
		).Run()
		if err != nil {
			os.Exit(0)
		}

		var targetDir = "./aeroflare-proxy"
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Extraction Directory").Description("Where should the proxy files be extracted?").Value(&targetDir),
			),
		).Run()
		if err != nil {
			os.Exit(0)
		}

		if targetDir == "" {
			targetDir = "./aeroflare-proxy"
		}

		os.MkdirAll(targetDir, 0755)

		PrintInfo(fmt.Sprintf("Downloading source for release %s...", releaseTag))
		tarURL := fmt.Sprintf("https://github.com/ItzEmoji/aeroflare/archive/refs/tags/%s.tar.gz", releaseTag)

		downloadCmd := exec.Command("sh", "-c", fmt.Sprintf("wget -qO- %s | tar -xz -C %s --strip-components=1", tarURL, targetDir))
		downloadCmd.Stdout = os.Stdout
		downloadCmd.Stderr = os.Stderr
		if err := downloadCmd.Run(); err != nil {
			PrintError(fmt.Sprintf("Failed to download or extract source: %v", err))
			os.Exit(1)
		}

		proxyDir := fmt.Sprintf("%s/proxy/no-webui-%s", targetDir, indexType)

		if _, err := os.Stat(proxyDir); os.IsNotExist(err) {
			PrintError(fmt.Sprintf("Proxy directory %s not found in the release", proxyDir))
			os.Exit(1)
		}

		// Collect Cloudflare credentials for API deployment
		var cfAccountId = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		var cfApiToken = os.Getenv("CLOUDFLARE_API_TOKEN")

		if cfAccountId == "" || cfApiToken == "" {
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Cloudflare Account ID").Value(&cfAccountId),
					huh.NewInput().Title("Cloudflare API Token").EchoMode(huh.EchoModePassword).Value(&cfApiToken),
				),
			).Run()
			if err != nil {
				os.Exit(0)
			}
		}

		if cfAccountId == "" || cfApiToken == "" {
			PrintError("Cloudflare Account ID and API Token are required to deploy.")
			os.Exit(1)
		}

		if indexType == "r2" {
			if r2Bucket == "" {
				err := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().Title("R2 Bucket Name").Value(&r2Bucket),
					),
				).Run()
				if err != nil {
					os.Exit(0)
				}
			}

			PrintInfo(fmt.Sprintf("Creating R2 bucket: %s", r2Bucket))
			if err := createR2BucketViaAPI(cfAccountId, cfApiToken, r2Bucket); err != nil {
				PrintError(fmt.Sprintf("Failed to create R2 bucket: %v", err))
				PrintInfo("You can create it manually later and retry.")
			} else {
				PrintSuccess(fmt.Sprintf("R2 bucket '%s' is ready.", r2Bucket))
			}
		}

		// Prompt for variables
		var nixcacheRepo = strings.TrimSuffix(repository, "/nix-cache")
		var nixcacheRegistry = registry
		var nixcacheUpstream = "https://cache.nixos.org"
		var nixcacheIndexTTL = "200"

		PrintInfo("Configuring Worker Variables...")
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("NIXCACHE_REPO").Value(&nixcacheRepo),
				huh.NewInput().Title("NIXCACHE_REGISTRY").Value(&nixcacheRegistry),
				huh.NewInput().Title("NIXCACHE_UPSTREAM").Value(&nixcacheUpstream),
				huh.NewInput().Title("NIXCACHE_INDEX_TTL").Value(&nixcacheIndexTTL),
			),
		).Run()
		if err != nil {
			os.Exit(0)
		}

		// Edit wrangler.toml (still used for local dev with wrangler, if the user wants it)
		wranglerPath := fmt.Sprintf("%s/wrangler.toml", proxyDir)
		content, err := os.ReadFile(wranglerPath)
		if err == nil {
			strContent := string(content)
			strContent = strings.Replace(strContent, `# NIXCACHE_REPO = "<NIXCACHE_REPO>"`, fmt.Sprintf(`NIXCACHE_REPO = "%s"`, nixcacheRepo), 1)
			strContent = strings.Replace(strContent, `# NIXCACHE_REGISTRY = "<NIXCACHE_REGISTRY>"`, fmt.Sprintf(`NIXCACHE_REGISTRY = "%s"`, nixcacheRegistry), 1)
			strContent = strings.Replace(strContent, `# NIXCACHE_UPSTREAM = "<NIXCACHE_UPSTREAM>"`, fmt.Sprintf(`NIXCACHE_UPSTREAM = "%s"`, nixcacheUpstream), 1)
			strContent = strings.Replace(strContent, `# NIXCACHE_INDEX_TTL = "<NIXCACHE_INDEX_TTL"`, fmt.Sprintf(`NIXCACHE_INDEX_TTL = "%s"`, nixcacheIndexTTL), 1)

			if indexType == "r2" && r2Bucket != "" {
				strContent = strings.Replace(strContent, `# [[r2_buckets]]`, `[[r2_buckets]]`, 1)
				strContent = strings.Replace(strContent, `# binding = "BUCKET"`, `binding = "BUCKET"`, 1)
				strContent = strings.Replace(strContent, `# bucket_name = "<bucket-name>"`, fmt.Sprintf(`bucket_name = "%s"`, r2Bucket), 1)
				strContent = strings.Replace(strContent, `# bucket_name = "<bucket-name>" `, fmt.Sprintf(`bucket_name = "%s"`, r2Bucket), 1)
			}

			os.WriteFile(wranglerPath, []byte(strContent), 0644)
		} else {
			PrintError(fmt.Sprintf("Could not read wrangler.toml: %v", err))
		}

		// Build the variables map for the API deployment
		workerVars := map[string]string{
			"NIXCACHE_REPO":      nixcacheRepo,
			"NIXCACHE_REGISTRY":  nixcacheRegistry,
			"NIXCACHE_UPSTREAM":  nixcacheUpstream,
			"NIXCACHE_INDEX_TTL": nixcacheIndexTTL,
		}

		// Parse compatibility_date from wrangler.toml
		compatDate := "2026-06-20" // fallback default
		if wranglerContent, err := os.ReadFile(wranglerPath); err == nil {
			for _, line := range strings.Split(string(wranglerContent), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "compatibility_date") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						compatDate = strings.Trim(strings.TrimSpace(parts[1]), "\"")
					}
				}
			}
		}

		// Parse worker name from wrangler.toml
		workerName := "aeroflare-proxy"
		if wranglerContent, err := os.ReadFile(wranglerPath); err == nil {
			for _, line := range strings.Split(string(wranglerContent), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name") && !strings.HasPrefix(line, "name =") || strings.HasPrefix(line, "name =") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						parsed := strings.Trim(strings.TrimSpace(parts[1]), "\"")
						if parsed != "" {
							workerName = parsed
						}
					}
					break
				}
			}
		}

		// Ask for Git integration
		var setupGit bool
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Would you like to push this proxy setup to a private Git repository?").
					Value(&setupGit),
			),
		).Run()
		if err != nil {
			os.Exit(0)
		}
		if setupGit {
			var gitProvider string
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select Git Provider").
						Options(
							huh.NewOption("GitHub", "github"),
							huh.NewOption("GitLab", "gitlab"),
						).
						Value(&gitProvider),
				),
			).Run()
			if err != nil {
				os.Exit(0)
			}

			var repoName = fmt.Sprintf("%s-proxy", strings.ReplaceAll(nixcacheRepo, "/", "-"))
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Repository Name").Value(&repoName),
				),
			).Run()
			if err != nil {
				os.Exit(0)
			}

			var gitToken string
			if gitProvider == "github" {
				gitToken = getGithubToken()
				if gitToken == "" {
					var useOAuth bool
					err = huh.NewForm(
						huh.NewGroup(
							huh.NewConfirm().
								Title("No GitHub token found. Authenticate via browser (OAuth Device Flow)?").
								Value(&useOAuth),
						),
					).Run()
					if err != nil {
						os.Exit(0)
					}

					if useOAuth {
						gitToken = getGithubTokenViaDeviceFlow()
					} else {
						huh.NewForm(huh.NewGroup(huh.NewInput().Title("GitHub Token").EchoMode(huh.EchoModePassword).Value(&gitToken))).Run()
					}
				}
			} else {
				gitToken = os.Getenv("GITLAB_TOKEN")
				if gitToken == "" {
					huh.NewForm(huh.NewGroup(huh.NewInput().Title("GitLab Token").EchoMode(huh.EchoModePassword).Value(&gitToken))).Run()
				}
			}

			if gitToken == "" {
				PrintError("Git token is required to proceed.")
				os.Exit(1)
			}

			var gitUsername string
			if gitProvider == "github" {
				reqUser, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
				reqUser.Header.Set("Authorization", "token "+gitToken)
				reqUser.Header.Set("Accept", "application/vnd.github.v3+json")
				respUser, errUser := http.DefaultClient.Do(reqUser)
				if errUser == nil && respUser.StatusCode == 200 {
					var userResult struct {
						Login string `json:"login"`
					}
					json.NewDecoder(respUser.Body).Decode(&userResult)
					respUser.Body.Close()
					gitUsername = userResult.Login
				}
			} else {
				reqUser, _ := http.NewRequest("GET", "https://gitlab.com/api/v4/user", nil)
				reqUser.Header.Set("Authorization", "Bearer "+gitToken)
				respUser, errUser := http.DefaultClient.Do(reqUser)
				if errUser == nil && respUser.StatusCode == 200 {
					var userResult struct {
						Username string `json:"username"`
					}
					json.NewDecoder(respUser.Body).Decode(&userResult)
					respUser.Body.Close()
					gitUsername = userResult.Username
				}
			}

			if gitUsername == "" {
				PrintError(fmt.Sprintf("Failed to fetch %s username. Is your token valid?", gitProvider))
				os.Exit(1)
			}
			PrintInfo(fmt.Sprintf("Configuring Git repository for %s...", gitProvider))

			exec.Command("git", "-C", proxyDir, "init").Run()
			exec.Command("git", "-C", proxyDir, "add", ".").Run()
			exec.Command("git", "-C", proxyDir, "commit", "-m", "Initial aeroflare proxy setup").Run()
			exec.Command("git", "-C", proxyDir, "branch", "-M", "main").Run()

			PrintInfo("Creating repository...")
			var cloneURL string
			if gitProvider == "github" {
				reqData := fmt.Sprintf(`{"name":"%s", "private": true}`, repoName)
				reqRepo, _ := http.NewRequest("POST", "https://api.github.com/user/repos", strings.NewReader(reqData))
				reqRepo.Header.Set("Authorization", "token "+gitToken)
				reqRepo.Header.Set("Accept", "application/vnd.github.v3+json")

				respRepo, httpErr := http.DefaultClient.Do(reqRepo)
				if httpErr == nil && respRepo.StatusCode >= 200 && respRepo.StatusCode < 300 {
					var result struct {
						CloneURL string `json:"clone_url"`
					}
					json.NewDecoder(respRepo.Body).Decode(&result)
					respRepo.Body.Close()
					cloneURL = strings.Replace(result.CloneURL, "https://", fmt.Sprintf("https://%s@", gitToken), 1)
				} else {
					status := 0
					var bodyMsg string
					if respRepo != nil {
						status = respRepo.StatusCode
						var bodyBytes []byte
						if respRepo.Body != nil {
							bodyBytes = make([]byte, 1024)
							n, _ := respRepo.Body.Read(bodyBytes)
							bodyMsg = string(bodyBytes[:n])
							respRepo.Body.Close()
						}
					}
					PrintError(fmt.Sprintf("Could not create GitHub repository automatically (HTTP %d). Response: %s", status, bodyMsg))
					PrintInfo("This usually happens if your GitHub token lacks the 'repo' scope (classic token) or repository creation permissions (fine-grained token).")
					os.Exit(1)
				}
			} else {
				reqData := fmt.Sprintf(`{"name":"%s", "visibility": "private"}`, repoName)
				reqRepo, _ := http.NewRequest("POST", "https://gitlab.com/api/v4/projects", strings.NewReader(reqData))
				reqRepo.Header.Set("Authorization", "Bearer "+gitToken)
				reqRepo.Header.Set("Content-Type", "application/json")
				respRepo, httpErr := http.DefaultClient.Do(reqRepo)
				if httpErr == nil && respRepo.StatusCode >= 200 && respRepo.StatusCode < 300 {
					var result struct {
						HttpUrlToRepo string `json:"http_url_to_repo"`
					}
					json.NewDecoder(respRepo.Body).Decode(&result)
					respRepo.Body.Close()
					cloneURL = strings.Replace(result.HttpUrlToRepo, "https://", fmt.Sprintf("https://oauth2:%s@", gitToken), 1)
				} else {
					status := 0
					var bodyMsg string
					if respRepo != nil {
						status = respRepo.StatusCode
						var bodyBytes []byte
						if respRepo.Body != nil {
							bodyBytes = make([]byte, 1024)
							n, _ := respRepo.Body.Read(bodyBytes)
							bodyMsg = string(bodyBytes[:n])
							respRepo.Body.Close()
						}
					}
					PrintError(fmt.Sprintf("Could not create GitLab repository automatically (HTTP %d). Response: %s", status, bodyMsg))
					os.Exit(1)
				}
			}

			if cloneURL != "" {
				exec.Command("git", "-C", proxyDir, "remote", "add", "origin", cloneURL).Run()

				PrintInfo("Pushing to remote...")
				pushCmd := exec.Command("git", "-C", proxyDir, "push", "-u", "origin", "main")
				pushCmd.Stdout = os.Stdout
				pushCmd.Stderr = os.Stderr
				if err := pushCmd.Run(); err != nil {
					PrintError("Failed to push to remote.")
					os.Exit(1)
				}
				PrintSuccess("Successfully pushed code!")
			}

			// Deploy the worker via the Cloudflare API
			PrintInfo("Deploying proxy to Cloudflare Workers via API...")
			scriptPath := fmt.Sprintf("%s/worker.js", proxyDir)
			scriptTag, deployErr := deployWorkerViaAPI(cfAccountId, cfApiToken, workerName, scriptPath, compatDate, workerVars, r2Bucket)
			if deployErr != nil {
				PrintError(fmt.Sprintf("Failed to deploy worker: %v", deployErr))
				os.Exit(1)
			}

			// Enable workers.dev subdomain
			enableWorkerRoute(cfAccountId, cfApiToken, workerName)

			workersDomain := getWorkersSubdomain(cfAccountId, cfApiToken)
			if workersDomain != "" {
				PrintSuccess(fmt.Sprintf("Worker deployed! Available at: https://%s.%s.workers.dev", workerName, workersDomain))
			} else {
				PrintSuccess("Worker deployed successfully!")
			}

			// Connect the Worker to the Git repo via Workers Builds API
			if cloneURL != "" && scriptTag != "" {
				cfTokenId := getCfApiTokenId(cfApiToken)
				if cfTokenId == "" {
					PrintError("Could not verify API token ID — skipping Workers Builds connection.")
					PrintInfo("You can connect Workers Builds manually in the dashboard:")
					PrintInfo(fmt.Sprintf("👉 https://dash.cloudflare.com/?to=/:account/workers/services/view/%s/builds", workerName))
				} else {
					err := connectWorkerToGitBuilds(cfAccountId, cfApiToken, cfTokenId, scriptTag, repoName, gitUsername, gitProvider)
					if err != nil {
						PrintError(fmt.Sprintf("Failed to connect Workers Builds: %v", err))
						PrintInfo("Your worker is deployed and your code is pushed, but automatic CI/CD is not set up.")
						PrintInfo("You can connect Workers Builds manually in the dashboard:")
						PrintInfo(fmt.Sprintf("👉 https://dash.cloudflare.com/?to=/:account/workers/services/view/%s/builds", workerName))
					} else {
						PrintSuccess("Workers Builds connected! Pushes to 'main' will automatically deploy.")
					}
				}
			}
		} else {
			// Deploy directly via the API (no git)
			PrintInfo("Deploying proxy to Cloudflare Workers via API...")
			scriptPath := fmt.Sprintf("%s/worker.js", proxyDir)
			if _, deployErr := deployWorkerViaAPI(cfAccountId, cfApiToken, workerName, scriptPath, compatDate, workerVars, r2Bucket); deployErr != nil {
				PrintError(fmt.Sprintf("Failed to deploy worker: %v", deployErr))
				os.Exit(1)
			}

			// Enable workers.dev subdomain
			enableWorkerRoute(cfAccountId, cfApiToken, workerName)

			workersDomain := getWorkersSubdomain(cfAccountId, cfApiToken)
			if workersDomain != "" {
				PrintSuccess(fmt.Sprintf("Worker deployed! Available at: https://%s.%s.workers.dev", workerName, workersDomain))
			} else {
				PrintSuccess("Successfully initialized and deployed Aeroflare proxy!")
			}
		}
	},
}

func init() {
	initCmd.Flags().StringVar(&initialIndexTypeFlag, "initial-index-type", "", "Initial index type (json or r2)")
	rootCmd.AddCommand(initCmd)
}
