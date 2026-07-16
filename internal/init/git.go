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
	"strings"

	"golang.org/x/crypto/nacl/box"
)

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub API HTTP %d: %s (ensure your token has the 'repo' scope)", resp.StatusCode, string(respBody))
	}

	var result struct {
		CloneURL string `json:"clone_url"`
	}
	_ = json.Unmarshal(respBody, &result)

	// Embed the token in the clone URL (as the "x-access-token" user) so the
	// later `git push` in pushToGitRepo can authenticate without a prompt.
	cloneURL := strings.Replace(result.CloneURL, "https://", fmt.Sprintf("https://x-access-token:%s@", token), 1)
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
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitLab API HTTP %d: %s (ensure your token has 'api' scope)", resp.StatusCode, string(respBody))
	}

	var result struct {
		HTTPUrlToRepo string `json:"http_url_to_repo"`
	}
	_ = json.Unmarshal(respBody, &result)

	// Embed the token in the clone URL (as the "oauth2" user, GitLab's
	// convention) for the same reason as createGitHubRepo above.
	cloneURL := strings.Replace(result.HTTPUrlToRepo, "https://", fmt.Sprintf("https://oauth2:%s@", token), 1)
	return cloneURL, nil
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
	defer func() { _ = resp.Body.Close() }()

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
		defer func() { _ = nsResp.Body.Close() }()

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
	defer func() { _ = createResp.Body.Close() }()

	if createResp.StatusCode < 200 || createResp.StatusCode >= 300 {
		body, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("failed to create GitLab project: HTTP %d: %s", createResp.StatusCode, string(body))
	}

	return nil
}

// setGitHubSecret sets a repository secret via the GitHub Actions Secrets
// API. GitHub requires secret values to be sealed with the repo's public
// key (libsodium/NaCl box) before upload, so this fetches that key first.
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
	defer func() { _ = pubKeyResp.Body.Close() }()

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
	defer func() { _ = putResp.Body.Close() }()

	if putResp.StatusCode != 201 && putResp.StatusCode != 204 {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("failed to set secret: HTTP %d: %s", putResp.StatusCode, string(body))
	}

	return nil
}
