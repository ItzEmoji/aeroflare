package setup

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedRequest is one request the fake API server received.
type capturedRequest struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// fakeAPI serves the given path -> JSON response mapping and records every
// request. Any path not in responses fails the test.
func fakeAPI(t *testing.T, responses map[string]string) (*httptest.Server, *[]capturedRequest) {
	t.Helper()

	var got []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		parsed := map[string]any{}
		_ = json.Unmarshal(body, &parsed)
		got = append(got, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Body:   parsed,
		})

		resp, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestCommitFilesToGitHub(t *testing.T) {
	srv, got := fakeAPI(t, map[string]string{
		"/repos/octo/cache-proxy/git/trees":   `{"sha":"tree-sha"}`,
		"/repos/octo/cache-proxy/git/commits": `{"sha":"commit-sha"}`,
		"/repos/octo/cache-proxy/git/refs":    `{}`,
	})

	old := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = old })

	files := map[string]string{
		"worker.js":     "export default {}",
		"wrangler.toml": "name = \"w\"",
	}
	if err := commitFilesToGitHub("tok", "octo", "cache-proxy", "main", "Initial commit", files); err != nil {
		t.Fatalf("commitFilesToGitHub() error = %v", err)
	}

	if len(*got) != 3 {
		t.Fatalf("got %d requests, want 3 (tree, commit, ref)", len(*got))
	}

	for _, r := range *got {
		if r.Auth != "token tok" {
			t.Errorf("%s: Authorization = %q, want %q", r.Path, r.Auth, "token tok")
		}
	}

	// The tree carries both files inline, so no separate blob upload is needed.
	tree, ok := (*got)[0].Body["tree"].([]any)
	if !ok || len(tree) != 2 {
		t.Fatalf("tree request body = %#v, want 2 entries", (*got)[0].Body["tree"])
	}
	first := tree[0].(map[string]any)
	if first["path"] != "worker.js" || first["content"] != "export default {}" {
		t.Errorf("first tree entry = %#v, want worker.js with its content", first)
	}
	if first["mode"] != "100644" || first["type"] != "blob" {
		t.Errorf("first tree entry mode/type = %v/%v, want 100644/blob", first["mode"], first["type"])
	}

	// The repo is empty, so the commit must be parentless and reference the
	// tree we just created.
	commit := (*got)[1].Body
	if commit["tree"] != "tree-sha" {
		t.Errorf("commit tree = %v, want tree-sha", commit["tree"])
	}
	if parents, ok := commit["parents"].([]any); !ok || len(parents) != 0 {
		t.Errorf("commit parents = %#v, want empty", commit["parents"])
	}
	if commit["message"] != "Initial commit" {
		t.Errorf("commit message = %v, want %q", commit["message"], "Initial commit")
	}

	// The branch is created, pointing at the new commit.
	ref := (*got)[2].Body
	if ref["ref"] != "refs/heads/main" || ref["sha"] != "commit-sha" {
		t.Errorf("ref request = %#v, want refs/heads/main -> commit-sha", ref)
	}
}

func TestCommitFilesToGitHubReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
	}))
	t.Cleanup(srv.Close)

	old := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = old })

	err := commitFilesToGitHub("tok", "octo", "cache-proxy", "main", "msg", map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("commitFilesToGitHub() on a 403: want error, got nil")
	}
	// The API's explanation is the actionable part, so it must survive.
	if !strings.Contains(err.Error(), "Resource not accessible") {
		t.Errorf("error = %q, want it to carry the API's message", err)
	}
}

func TestCommitFilesToGitLab(t *testing.T) {
	// The project path is URL-escaped into a single path segment.
	srv, got := fakeAPI(t, map[string]string{
		"/projects/octo/cache-proxy/repository/commits": `{}`,
	})

	old := gitlabAPIBase
	gitlabAPIBase = srv.URL
	t.Cleanup(func() { gitlabAPIBase = old })

	files := map[string]string{"wrangler.toml": "name = \"w\""}
	if err := commitFilesToGitLab("tok", "octo/cache-proxy", "main", "Initial commit", files); err != nil {
		t.Fatalf("commitFilesToGitLab() error = %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("got %d requests, want 1", len(*got))
	}
	body := (*got)[0].Body
	if body["branch"] != "main" || body["commit_message"] != "Initial commit" {
		t.Errorf("commit request = %#v, want branch main / message Initial commit", body)
	}

	actions, ok := body["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions = %#v, want 1 entry", body["actions"])
	}
	a := actions[0].(map[string]any)
	if a["action"] != "create" || a["file_path"] != "wrangler.toml" {
		t.Errorf("action = %#v, want create wrangler.toml", a)
	}
}
