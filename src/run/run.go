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

	"aeroflare/src/proxy"
	"aeroflare/src/ui"
)

type RunConfig struct {
	Command []string
}

func DisplaySummary(cfg *RunConfig) {
	cmdStr := strings.Join(cfg.Command, " ")
	ui.PrintSummaryBox("Run Summary", []ui.BoxField{
		{Label: "Command", Value: cmdStr},
		{Label: "Action", Value: "Execute command via proxy"},
	})
}

// ExecuteCommand starts proxy, runs cmd, and returns harvested store paths
func ExecuteCommand(cfg *RunConfig, registry, repository, indexDir, githubToken string) ([]string, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("command is empty")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Suppress proxy INFO logs so they don't pollute the command output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(logger)

	port, err := proxy.StartProxy(ctx, 0, "127.0.0.1", registry, repository, indexDir, "", 300, []string{"https://cache.nixos.org"}, githubToken)
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
	nixConfig := os.Getenv("NIX_CONFIG")
	if nixConfig != "" {
		nixConfig += "\n"
	}
	nixConfig += fmt.Sprintf("extra-substituters = http://127.0.0.1:%d\naccept-flake-config = true", port)
	env = append(env, "NIX_CONFIG="+nixConfig)
	cmdToRun.Env = env

	err = cmdToRun.Run()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w", err)
	}

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
