// Package run executes a command through a local Nix proxy substituter and
// harvests the resulting store paths for caching or pushing.
package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/proxy"
)

// RunConfig holds the command line to be executed via the Nix proxy.
type RunConfig struct {
	Command []string
}

// DisplaySummary prints a formatted summary of the command that will be run.
func DisplaySummary(cfg *RunConfig) {
	cmdStr := strings.Join(cfg.Command, " ")
	ui.PrintSummaryBox("Run Summary", []ui.BoxField{
		{Label: "Command", Value: cmdStr},
		{Label: "Action", Value: "Execute command via proxy"},
	})
}

// buildNixConfig appends the proxy substituter to any existing NIX_CONFIG value.
// It deliberately does NOT set accept-flake-config, which would silently trust
// substituters and keys defined by arbitrary flakes — a security risk when running
// untrusted build scripts.
func buildNixConfig(existing string, port int) string {
	cfg := existing
	if cfg != "" {
		cfg += "\n"
	}
	return cfg + fmt.Sprintf("extra-substituters = http://127.0.0.1:%d", port)
}

// ExecuteCommand starts a proxy server, runs cfg.Command with the proxy
// substituter set in NIX_CONFIG, captures stdout, and returns extracted Nix
// store paths (lines starting with /nix/store/ and not prefixed with #).
// The proxy is configured with the given registry, repository, and registry
// credential for cache interactions.
func ExecuteCommand(cfg *RunConfig, registry, repository string, auth authn.Authenticator) ([]string, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("command is empty")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Suppress proxy INFO logs so they don't pollute the command output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(logger)

	port, err := proxy.StartProxy(ctx, 0, "127.0.0.1", registry, repository, []string{"https://cache.nixos.org"}, auth)
	if err != nil {
		return nil, fmt.Errorf("failed to start proxy: %w", err)
	}

	fmt.Printf("Started background proxy on 127.0.0.1:%d\n", port)

	var stdoutBuf bytes.Buffer
	cmdToRun := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmdToRun.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmdToRun.Stderr = os.Stderr
	cmdToRun.Stdin = os.Stdin

	env := os.Environ()
	env = append(env, "NIX_CONFIG="+buildNixConfig(os.Getenv("NIX_CONFIG"), port))
	cmdToRun.Env = env

	err = cmdToRun.Run()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w", err)
	}

	// Extract store paths from command output: split into lines, trim whitespace,
	// skip empty lines and comments (#), and keep only lines starting with /nix/store/.
	var targetPaths []string
	lines := strings.Split(stdoutBuf.String(), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && strings.HasPrefix(line, "/nix/store/") {
			targetPaths = append(targetPaths, line)
		}
	}

	return targetPaths, nil
}
