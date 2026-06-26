package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
)

// deployWorkerViaAPI uploads a worker script to Cloudflare Workers.
func deployWorkerViaAPI(cfAccountID, cfApiToken, workerName, scriptPath, compatDate string, vars map[string]string, r2Bucket string) (string, error) {
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("read worker script: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Build bindings from environment variables and R2 bucket.
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

	// Metadata part.
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metaHeader.Set("Content-Type", "application/json")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return "", fmt.Errorf("create metadata part: %w", err)
	}
	metaPart.Write(metadataJSON)

	// Worker script part.
	scriptHeader := make(textproto.MIMEHeader)
	scriptHeader.Set("Content-Disposition", `form-data; name="worker.js"; filename="worker.js"`)
	scriptHeader.Set("Content-Type", "application/javascript+module")
	scriptPart, err := writer.CreatePart(scriptHeader)
	if err != nil {
		return "", fmt.Errorf("create script part: %w", err)
	}
	scriptPart.Write(scriptContent)
	writer.Close()

	req, err := http.NewRequest("PUT",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", cfAccountID, workerName),
		&body,
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
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
		return "", fmt.Errorf("cloudflare API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			Tag string `json:"tag"`
		} `json:"result"`
	}
	json.Unmarshal(respBody, &result)
	return result.Result.Tag, nil
}

// enableWorkerRoute enables the workers.dev subdomain route for a worker.
func enableWorkerRoute(cfAccountID, cfApiToken, workerName string) error {
	payload, _ := json.Marshal(map[string]interface{}{"enabled": true})

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s/subdomain", cfAccountID, workerName),
		bytes.NewReader(payload),
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

	// Subdomain may already be enabled; not a fatal error.
	return nil
}

// getWorkersSubdomain fetches the workers.dev subdomain for the account.
func getWorkersSubdomain(cfAccountID, cfApiToken string) string {
	req, err := http.NewRequest("GET",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/subdomain", cfAccountID),
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

// createR2BucketViaAPI creates an R2 bucket. Returns nil if it already exists.
func createR2BucketViaAPI(cfAccountID, cfApiToken, bucketName string) error {
	payload, _ := json.Marshal(map[string]string{"name": bucketName})

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/r2/buckets", cfAccountID),
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
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
		if resp.StatusCode == 409 {
			printInfo(fmt.Sprintf("R2 bucket '%s' already exists, continuing.", bucketName))
			return nil
		}
		return fmt.Errorf("cloudflare API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// getCfApiTokenID retrieves the token ID for the current API token.
func getCfApiTokenID(cfApiToken string) string {
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

// connectWorkerToGitBuilds links a deployed Worker to a Git repository for CI/CD.
func connectWorkerToGitBuilds(cfAccountID, cfApiToken, cfTokenID, scriptTag, repoName, gitUsername, gitProvider string) error {
	printInfo("Connecting repository to Cloudflare Workers Builds...")

	repoUUID, err := createRepoConnection(cfAccountID, cfApiToken, repoName, gitUsername, gitProvider)
	if err != nil {
		return fmt.Errorf("repo connection: %w", err)
	}
	printSuccess(fmt.Sprintf("Repository connection created: %s", repoUUID))

	printInfo("Creating build token...")
	tokenUUID, err := createBuildToken(cfAccountID, cfApiToken, cfTokenID, repoName)
	if err != nil {
		return fmt.Errorf("build token: %w", err)
	}

	printInfo("Creating build trigger...")
	if err := createBuildTrigger(cfAccountID, cfApiToken, scriptTag, repoUUID, tokenUUID); err != nil {
		return fmt.Errorf("build trigger: %w", err)
	}

	return nil
}

// createRepoConnection upserts a repository connection for Workers Builds.
func createRepoConnection(cfAccountID, cfApiToken, repoName, gitUsername, gitProvider string) (string, error) {
	payload := map[string]string{
		"repo_id":               repoName,
		"repo_name":             repoName,
		"provider_type":         gitProvider,
		"provider_account_id":   gitUsername,
		"provider_account_name": gitUsername,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("PUT",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/repos/connections", cfAccountID),
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
		var errResp struct {
			Errors []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && len(errResp.Errors) > 0 {
			msg := errResp.Errors[0].Message
			if errResp.Errors[0].Code == 8000008 {
				msg = "Cloudflare is not authorized to access your Git repository. Install the Cloudflare GitHub App first."
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
func createBuildToken(cfAccountID, cfApiToken, cfTokenID, repoName string) (string, error) {
	payload := map[string]string{
		"build_token_name":    fmt.Sprintf("aeroflare-%s", repoName),
		"build_token_secret":  cfApiToken,
		"cloudflare_token_id": cfTokenID,
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/tokens", cfAccountID),
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
func createBuildTrigger(cfAccountID, cfApiToken, scriptTag, repoConnectionUUID, buildTokenUUID string) error {
	payload := map[string]interface{}{
		"external_script_id":   scriptTag,
		"repo_connection_uuid": repoConnectionUUID,
		"build_token_uuid":     buildTokenUUID,
		"trigger_name":         "Production Deploy",
		"build_command":        "",
		"deploy_command":       "npx wrangler deploy",
		"root_directory":       "/",
		"branch_includes":      []string{"main"},
		"branch_excludes":      []string{},
		"path_includes":        []string{"*"},
		"path_excludes":        []string{},
	}
	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/builds/triggers", cfAccountID),
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
