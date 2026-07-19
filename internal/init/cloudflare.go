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

// deployWorkerViaAPI uploads a worker script to Cloudflare Workers using the
// multipart "module upload" format, and returns the deployed script's tag.
func deployWorkerViaAPI(cfAccountID, cfApiToken, workerName, scriptPath, compatDate string, vars, secrets map[string]string) (string, error) {
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("read worker script: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Build bindings from environment variables (plain text) and secrets
	// (encrypted at rest, never returned by the API).
	bindings := []map[string]interface{}{}
	for k, v := range vars {
		bindings = append(bindings, map[string]interface{}{
			"type": "plain_text",
			"name": k,
			"text": v,
		})
	}
	for k, v := range secrets {
		bindings = append(bindings, map[string]interface{}{
			"type": "secret_text",
			"name": k,
			"text": v,
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
	_, _ = metaPart.Write(metadataJSON)

	// Worker script part.
	scriptHeader := make(textproto.MIMEHeader)
	scriptHeader.Set("Content-Disposition", `form-data; name="worker.js"; filename="worker.js"`)
	scriptHeader.Set("Content-Type", "application/javascript+module")
	scriptPart, err := writer.CreatePart(scriptHeader)
	if err != nil {
		return "", fmt.Errorf("create script part: %w", err)
	}
	_, _ = scriptPart.Write(scriptContent)
	_ = writer.Close()

	req, err := http.NewRequest("PUT",
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", cfAccountID, workerName),
		&body,
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfApiToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloudflare API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return parseWorkerDeployTag(respBody)
}

// parseWorkerDeployTag extracts the deployed script's tag from a successful
// Cloudflare deploy response. A body that doesn't parse is surfaced as an
// error rather than silently yielding an empty tag that reads as success.
func parseWorkerDeployTag(respBody []byte) (string, error) {
	var result struct {
		Result struct {
			Tag string `json:"tag"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse worker deploy response: %w", err)
	}
	return result.Result.Tag, nil
}

// enableWorkerRoute enables the workers.dev subdomain route for a worker so
// it is reachable at https://<worker>.<subdomain>.workers.dev immediately
// after deployment. The response body/status is intentionally not
// inspected: the subdomain may already be enabled from a prior run, and
// callers (see configureWorker) already treat any error here as a
// non-fatal warning.
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

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

	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

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
