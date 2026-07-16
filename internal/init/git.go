package setup

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/crypto/nacl/box"
)

// githubAPIBase and gitlabAPIBase are the API roots. They are variables so
// tests can point them at a local server.
var (
	githubAPIBase = "https://api.github.com"
	gitlabAPIBase = "https://gitlab.com/api/v4"
)

// githubJSON performs a GitHub API request with the standard headers, and
// decodes a successful response into out. payload and out may each be nil, for
// a request with no body and a response whose body isn't needed.
func githubJSON(method, url, token string, payload, out any) error {
	return apiJSON(method, url, payload, out, func(req *http.Request) {
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
	})
}

// gitlabJSON is gitlabAPIBase's equivalent of githubJSON.
func gitlabJSON(method, url, token string, payload, out any) error {
	return apiJSON(method, url, payload, out, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("PRIVATE-TOKEN", token)
	})
}

// apiJSON sends payload as JSON (when non-nil), applies auth via setAuth, and
// unmarshals a 2xx response body into out (when non-nil). Non-2xx responses
// become an error carrying the body, which is where these APIs explain
// themselves (a missing scope, a name already taken).
func apiJSON(method, url string, payload, out any, setAuth func(*http.Request)) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	setAuth(req)
	req.Header.Set("User-Agent", "aeroflare/1.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// commitFilesToGitHub creates a single commit containing files (keyed by repo
// path) on branch of owner/repo, via the Git Data API.
//
// init only calls this against the repository it just created, which is empty:
// the commit therefore has no parents, and the branch ref is created rather
// than fast-forwarded.
func commitFilesToGitHub(token, owner, repo, branch, message string, files map[string]string) error {
	type treeEntry struct {
		Path string `json:"path"`
		// 100644 is git's mode for a non-executable file blob.
		Mode string `json:"mode"`
		Type string `json:"type"`
		// Inline content, so the API writes the blob for us and we don't have
		// to create each one in a separate request first.
		Content string `json:"content"`
	}

	entries := make([]treeEntry, 0, len(files))
	for _, path := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, treeEntry{Path: path, Mode: "100644", Type: "blob", Content: files[path]})
	}

	var tree struct {
		SHA string `json:"sha"`
	}
	if err := githubJSON("POST", fmt.Sprintf("%s/repos/%s/%s/git/trees", githubAPIBase, owner, repo), token,
		map[string]any{"tree": entries}, &tree); err != nil {
		return fmt.Errorf("create tree: %w", err)
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := githubJSON("POST", fmt.Sprintf("%s/repos/%s/%s/git/commits", githubAPIBase, owner, repo), token,
		map[string]any{"message": message, "tree": tree.SHA, "parents": []string{}}, &commit); err != nil {
		return fmt.Errorf("create commit: %w", err)
	}

	if err := githubJSON("POST", fmt.Sprintf("%s/repos/%s/%s/git/refs", githubAPIBase, owner, repo), token,
		map[string]any{"ref": "refs/heads/" + branch, "sha": commit.SHA}, nil); err != nil {
		return fmt.Errorf("create branch %q: %w", branch, err)
	}
	return nil
}

// commitFilesToGitLab creates a single commit containing files (keyed by repo
// path) on branch of project, a "namespace/name" path. The Commits API creates
// branch when the project is empty, which is the case init uses.
func commitFilesToGitLab(token, project, branch, message string, files map[string]string) error {
	type action struct {
		Action   string `json:"action"`
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}

	actions := make([]action, 0, len(files))
	for _, path := range slices.Sorted(maps.Keys(files)) {
		actions = append(actions, action{Action: "create", FilePath: path, Content: files[path]})
	}

	endpoint := fmt.Sprintf("%s/projects/%s/repository/commits", gitlabAPIBase, url.PathEscape(project))
	payload := map[string]any{"branch": branch, "commit_message": message, "actions": actions}
	if err := gitlabJSON("POST", endpoint, token, payload, nil); err != nil {
		return fmt.Errorf("create commit on %s: %w", branch, err)
	}
	return nil
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

	return result.CloneURL, nil
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

	return result.HTTPUrlToRepo, nil
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
