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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloudflare API HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result struct {
			Tag string `json:"tag"`
		} `json:"result"`
	}
	_ = json.Unmarshal(respBody, &result)
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
	defer func() { _ = resp.Body.Close() }()

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

