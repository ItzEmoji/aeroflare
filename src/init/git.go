package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const githubOAuthClientID = "Ov23liIJyLpd2Cse5gne"
const gitlabOAuthClientID = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0" // REPLACE_ME_GITLAB_CLIENT_ID

// detectGitHubToken returns a GitHub token from common environment variables.
func detectGitHubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

// detectGitLabToken returns a GitLab token from the environment.
func detectGitLabToken() string {
	return os.Getenv("GITLAB_TOKEN")
}

// getGitHubUsername fetches the authenticated user's login.
func getGitHubUsername(token string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var u struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// getGitLabUsername fetches the authenticated user's username.
func getGitLabUsername(token string) (string, error) {
	req, err := http.NewRequest("GET", "https://gitlab.com/api/v4/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitLab API returned HTTP %d (Ensure your token has 'api' or 'read_user' scope)", resp.StatusCode)
	}

	var u struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return u.Username, nil
}

// createGitHubRepo creates a private GitHub repository and returns the clone URL.
func createGitHubRepo(token, repoName string) (string, error) {
	payload := fmt.Sprintf(`{"name":%q, "private": true}`, repoName)

	req, err := http.NewRequest("POST", "https://api.github.com/user/repos", strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "aeroflare/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub API HTTP %d: %s\nEnsure your token has the 'repo' scope.", resp.StatusCode, string(respBody))
	}

	var result struct {
		CloneURL string `json:"clone_url"`
	}
	json.Unmarshal(respBody, &result)

	// Embed token in clone URL for authenticated push.
	cloneURL := strings.Replace(result.CloneURL, "https://", fmt.Sprintf("https://%s@", token), 1)
	return cloneURL, nil
}

// createGitLabRepo creates a private GitLab repository and returns the clone URL.
func createGitLabRepo(token, repoName string) (string, error) {
	payload := fmt.Sprintf(`{"name":%q, "visibility": "private"}`, repoName)

	req, err := http.NewRequest("POST", "https://gitlab.com/api/v4/projects", strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("User-Agent", "aeroflare/1.0")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitLab API HTTP %d: %s\nEnsure your token has 'api' scope.", resp.StatusCode, string(respBody))
	}

	var result struct {
		HTTPUrlToRepo string `json:"http_url_to_repo"`
	}
	json.Unmarshal(respBody, &result)

	cloneURL := strings.Replace(result.HTTPUrlToRepo, "https://", fmt.Sprintf("https://oauth2:%s@", token), 1)
	return cloneURL, nil
}

// githubDeviceFlow authenticates via GitHub OAuth Device Flow.
func githubDeviceFlow() string {
	reqBody := strings.NewReader(fmt.Sprintf("client_id=%s&scope=repo write:packages read:packages", githubOAuthClientID))
	req, err := http.NewRequest("POST", "https://github.com/login/device/code", reqBody)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return ""
	}

	fmt.Println()
	printInfo(fmt.Sprintf("Open your browser to: %s", deviceResp.VerificationURI))
	printInfo(fmt.Sprintf("Enter the code: %s", deviceResp.UserCode))
	printInfo("Waiting for authorization...")

	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	for {
		time.Sleep(interval)
		tokenBody := strings.NewReader(fmt.Sprintf(
			"client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code",
			githubOAuthClientID, deviceResp.DeviceCode,
		))
		tokenReq, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", tokenBody)
		if err != nil {
			continue
		}
		tokenReq.Header.Set("Accept", "application/json")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			continue
		}

		var result struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		json.NewDecoder(tokenResp.Body).Decode(&result)
		tokenResp.Body.Close()

		if result.AccessToken != "" {
			printSuccess("GitHub authentication successful!")
			return result.AccessToken
		}

		if result.Error != "authorization_pending" && result.Error != "slow_down" {
			printError(fmt.Sprintf("GitHub OAuth error: %s", result.Error))
			return ""
		}
	}
}

// gitlabDeviceFlow authenticates via GitLab OAuth Device Flow.
func gitlabDeviceFlow() string {
	reqBody := strings.NewReader(fmt.Sprintf("client_id=%s", gitlabOAuthClientID))
	
	ctxInit, cancelInit := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelInit()

	req, err := http.NewRequestWithContext(ctxInit, "POST", "https://gitlab.com/oauth/authorize_device", reqBody)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		printError(fmt.Sprintf("Failed to initialize GitLab device flow: %v", err))
		return ""
	}
	defer resp.Body.Close()

	var deviceResp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		printError(fmt.Sprintf("Failed to initialize GitLab device flow: %v", err))
		return ""
	}

	fmt.Println()
	printInfo(fmt.Sprintf("Open your browser to: %s", deviceResp.VerificationURIComplete))
	printInfo(fmt.Sprintf("Ensure the code matches: %s", deviceResp.UserCode))
	printInfo("Waiting for authorization...")

	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	expiresIn := deviceResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 600
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(expiresIn)*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			printError("GitLab OAuth device flow timed out.")
			return ""
		case <-time.After(interval):
		}

		tokenBody := strings.NewReader(fmt.Sprintf(
			"client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code",
			gitlabOAuthClientID, deviceResp.DeviceCode,
		))
		tokenReq, err := http.NewRequestWithContext(ctx, "POST", "https://gitlab.com/oauth/token", tokenBody)
		if err != nil {
			continue
		}
		tokenReq.Header.Set("Accept", "application/json")
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			continue
		}

		var result struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		err = json.NewDecoder(tokenResp.Body).Decode(&result)
		tokenResp.Body.Close()
		if err != nil {
			continue
		}

		if result.AccessToken != "" {
			printSuccess("GitLab authentication successful!")
			return result.AccessToken
		}

		if result.Error != "authorization_pending" && result.Error != "slow_down" {
			printError(fmt.Sprintf("GitLab OAuth error: %s", result.Error))
			return ""
		}
	}
}

// ensureGitLabProjectExists checks if the base project exists and creates it if it doesn't.
func ensureGitLabProjectExists(token, fullProjectName string) error {
	apiPath := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", url.PathEscape(fullProjectName))
	req, err := http.NewRequest("GET", apiPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil // exists
	}
	if resp.StatusCode != 404 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitLab API HTTP %d: %s", resp.StatusCode, string(body))
	}

	// 404: Need to create it.
	parts := strings.SplitN(fullProjectName, "/", 2)
	var payloadStr string
	if len(parts) == 1 {
		payloadStr = fmt.Sprintf(`{"name":%q, "visibility": "private"}`, parts[0])
	} else {
		namespace := parts[0]
		name := parts[1]

		nsReq, err := http.NewRequest("GET", "https://gitlab.com/api/v4/namespaces/"+url.PathEscape(namespace), nil)
		if err != nil {
			return err
		}
		nsReq.Header.Set("Authorization", "Bearer "+token)
		nsReq.Header.Set("PRIVATE-TOKEN", token)

		nsResp, err := http.DefaultClient.Do(nsReq)
		if err != nil {
			return err
		}
		defer nsResp.Body.Close()

		if nsResp.StatusCode != 200 {
			nsBody, _ := io.ReadAll(nsResp.Body)
			return fmt.Errorf("failed to find GitLab namespace %q: HTTP %d: %s", namespace, nsResp.StatusCode, string(nsBody))
		}

		var ns struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(nsResp.Body).Decode(&ns); err != nil {
			return err
		}

		payloadStr = fmt.Sprintf(`{"name":%q, "namespace_id": %d, "visibility": "private"}`, name, ns.ID)
	}

	createReq, err := http.NewRequest("POST", "https://gitlab.com/api/v4/projects", strings.NewReader(payloadStr))
	if err != nil {
		return err
	}
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("PRIVATE-TOKEN", token)
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		return err
	}
	defer createResp.Body.Close()

	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		body, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("failed to create GitLab project: HTTP %d: %s", createResp.StatusCode, string(body))
	}

	return nil
}
