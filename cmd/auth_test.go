package cmd

import (
	"bytes"
	"errors"
	"testing"
	"github.com/spf13/cobra"
	"aeroflare/src/secrets"
)

type mockManager struct {
	data map[string]string
	err  error
}

func (m *mockManager) Set(key, value string) error {
	if m.err != nil {
		return m.err
	}
	m.data[key] = value
	return nil
}

func (m *mockManager) Get(key string) (string, error) {
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return "", secrets.ErrNotFound
}

func (m *mockManager) List() ([]string, error) {
	var keys []string
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *mockManager) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err = root.Execute()

	return buf.String(), err
}

func TestAuthCmdExists(t *testing.T) {
	if authCmd.Use != "auth" {
		t.Errorf("Expected auth command")
	}
}

func TestAuthCmdBehavior(t *testing.T) {
	mock := &mockManager{data: make(map[string]string)}
	SecretsManager = mock
	defer func() { SecretsManager = nil }() // reset

	// Reset variables since they are package level
	defer func() {
		globalGithubToken = ""
		globalGitlabToken = ""
		globalCfToken = ""
		globalCfUserID = ""
	}()

	_, err := executeCommand(rootCmd, "auth", "login", "--github-token=test-gh", "--gitlab-token=test-gl", "--cf-token=test-cf", "--cf-user-id=test-id")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if mock.data["github-token"] != "test-gh" {
		t.Errorf("Expected github-token to be 'test-gh', got %s", mock.data["github-token"])
	}

	if mock.data["gitlab-token"] != "test-gl" {
		t.Errorf("Expected gitlab-token to be 'test-gl', got %s", mock.data["gitlab-token"])
	}

	if mock.data["cf-token"] != "test-cf" {
		t.Errorf("Expected cf-token to be 'test-cf', got %s", mock.data["cf-token"])
	}

	if mock.data["cf-user-id"] != "test-id" {
		t.Errorf("Expected cf-user-id to be 'test-id', got %s", mock.data["cf-user-id"])
	}
}

func TestAuthCmdError(t *testing.T) {
	mock := &mockManager{data: make(map[string]string), err: errors.New("mock error")}
	SecretsManager = mock
	defer func() { SecretsManager = nil }()

	defer func() {
		globalGithubToken = ""
		globalGitlabToken = ""
		globalCfToken = ""
		globalCfUserID = ""
	}()

	_, err := executeCommand(rootCmd, "auth", "login", "--github-token=fail")
	if err == nil {
		t.Fatalf("Expected an error, got nil")
	}
}

func TestAuthSetCmdBehavior(t *testing.T) {
	mock := &mockManager{data: make(map[string]string)}
	SecretsManager = mock
	defer func() { SecretsManager = nil }()

	_, err := executeCommand(rootCmd, "auth", "set", "custom-key", "custom-value")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if mock.data["custom-key"] != "custom-value" {
		t.Errorf("Expected custom-key to be 'custom-value', got %s", mock.data["custom-key"])
	}
}
