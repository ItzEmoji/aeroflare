package run

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDisplaySummary(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := &RunConfig{
		Command: []string{"echo", "hello"},
	}
	DisplaySummary(cfg)

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "│  Command: echo hello") {
		t.Errorf("Expected output to contain '│  Command: echo hello', got: %s", output)
	}
}

func TestExecuteCommand_EmptyCommand(t *testing.T) {
	cfg := &RunConfig{
		Command: []string{},
	}
	_, err := ExecuteCommand(cfg, "registry", "repo", "dir", "token")
	if err == nil {
		t.Fatalf("Expected error for empty command, got nil")
	}
	if !strings.Contains(err.Error(), "command is empty") {
		t.Errorf("Expected error to mention 'command is empty', got: %v", err)
	}
}
