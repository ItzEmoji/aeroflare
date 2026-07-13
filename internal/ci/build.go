package ci

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// scrapeStorePaths extracts /nix/store/... paths from command stdout: trimmed,
// non-empty, non-comment lines beginning with /nix/store/.
func scrapeStorePaths(stdout string) []string {
	var paths []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && strings.HasPrefix(line, "/nix/store/") {
			paths = append(paths, line)
		}
	}
	return paths
}

// proxyNixConfig appends the proxy substituter to any existing NIX_CONFIG value.
// port <= 0 returns existing unchanged. It deliberately never sets
// accept-flake-config (which would trust arbitrary flake substituters/keys).
func proxyNixConfig(existing string, port int) string {
	if port <= 0 {
		return existing
	}
	cfg := existing
	if cfg != "" {
		cfg += "\n"
	}
	return cfg + fmt.Sprintf("extra-substituters = http://127.0.0.1:%d", port)
}

// dedupPaths returns in with duplicate store paths removed, preserving order.
func dedupPaths(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// BuildInstallable runs `nix build <installable> --print-out-paths` and returns
// the resulting store paths. When proxyPort > 0, the build is routed through the
// local proxy substituter at 127.0.0.1:<proxyPort>. Build logs stream to
// stderr/stdout.
func BuildInstallable(installable string, proxyPort int) ([]string, error) {
	var stdout bytes.Buffer
	cmd := exec.Command("nix", "build", installable, "--print-out-paths")
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if nc := proxyNixConfig(os.Getenv("NIX_CONFIG"), proxyPort); nc != "" {
		cmd.Env = append(cmd.Env, "NIX_CONFIG="+nc)
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nix build %s: %w", installable, err)
	}
	paths := scrapeStorePaths(stdout.String())
	if len(paths) == 0 {
		return nil, fmt.Errorf("nix build %s produced no store paths", installable)
	}
	return paths, nil
}
