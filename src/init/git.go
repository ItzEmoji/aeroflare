package setup

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"
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

// setGitHubSecret encrypts and sets a repository secret on GitHub via Actions Secrets API.
func setGitHubSecret(token, owner, repo, secretName, secretValue string) error {
	pubKeyReq, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/public-key", owner, repo), nil)
	if err != nil {
		return err
	}
	pubKeyReq.Header.Set("Authorization", "token "+token)
	pubKeyReq.Header.Set("Accept", "application/vnd.github.v3+json")
	pubKeyReq.Header.Set("User-Agent", "aeroflare/1.0")

	pubKeyResp, err := http.DefaultClient.Do(pubKeyReq)
	if err != nil {
		return err
	}
	defer pubKeyResp.Body.Close()

	if pubKeyResp.StatusCode != 200 {
		return fmt.Errorf("failed to get public key: HTTP %d", pubKeyResp.StatusCode)
	}

	var pubKey struct {
		KeyId string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := json.NewDecoder(pubKeyResp.Body).Decode(&pubKey); err != nil {
		return err
	}

	decodedPubKey, err := base64.StdEncoding.DecodeString(pubKey.Key)
	if err != nil || len(decodedPubKey) != 32 {
		return fmt.Errorf("invalid public key")
	}

	var recipientKey [32]byte
	copy(recipientKey[:], decodedPubKey)

	encryptedBytes, err := box.SealAnonymous(nil, []byte(secretValue), &recipientKey, rand.Reader)
	if err != nil {
		return err
	}
	encryptedValue := base64.StdEncoding.EncodeToString(encryptedBytes)

	payload := map[string]string{
		"encrypted_value": encryptedValue,
		"key_id":          pubKey.KeyId,
	}
	payloadBytes, _ := json.Marshal(payload)

	putReq, err := http.NewRequest("PUT", fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/%s", owner, repo, secretName), bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	putReq.Header.Set("Authorization", "token "+token)
	putReq.Header.Set("Accept", "application/vnd.github.v3+json")
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("User-Agent", "aeroflare/1.0")

	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != 201 && putResp.StatusCode != 204 {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("failed to set secret: HTTP %d: %s", putResp.StatusCode, string(body))
	}

	return nil
}
