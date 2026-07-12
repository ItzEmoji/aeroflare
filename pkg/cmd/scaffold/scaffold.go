// Package scaffold implements `aeroflare scaffold`, which downloads an
// Aeroflare release and generates local project files for a worker: source,
// wrangler.toml, and configuration.
package scaffold

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/itzemoji/aeroflare/internal/oci"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies scaffoldRun needs.
type Options struct {
	IO *iostreams.IOStreams

	Release   string
	OutputDir string
}

// NewCmdScaffold builds the `aeroflare scaffold` command.
func NewCmdScaffold(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "scaffold",
		Short: "Generate local project files for an Aeroflare worker",
		Long: `Download an Aeroflare release and scaffold a local project directory
containing the worker source, wrangler.toml, and configuration files.

This command generates local files only — it does not create or modify
any remote infrastructure. Use 'aeroflare init' for that.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return scaffoldRun(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Release, "release", "", "Release tag to download (default: prompt)")
	cmd.Flags().StringVar(&opts.OutputDir, "output-dir", "./aeroflare-proxy", "Directory to scaffold into")

	return cmd
}

func scaffoldRun(opts *Options) error {
	// Fetch available releases.
	opts.IO.Info("Fetching available releases...")
	resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases")
	if err != nil {
		return fmt.Errorf("Failed to fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return fmt.Errorf("Failed to decode releases: %w", err)
	}
	if len(releases) == 0 {
		return fmt.Errorf("No releases found.")
	}

	releaseTag := opts.Release
	if releaseTag == "" {
		var options []huh.Option[string]
		for _, r := range releases {
			options = append(options, huh.NewOption(r.TagName, r.TagName))
		}

		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which release of Aeroflare do you want to use?").
					Options(options...).
					Value(&releaseTag),
			),
		).Run()
		if err != nil {
			return nil
		}
	}

	targetDir := opts.OutputDir
	if targetDir == "" {
		targetDir = "./aeroflare-proxy"
	}

	// Prompt for extraction directory.
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Output directory").
				Description("Where should the project files be extracted?").
				Value(&targetDir),
		),
	).Run()
	if err != nil {
		return nil
	}

	if targetDir == "" {
		targetDir = "./aeroflare-proxy"
	}
	_ = os.MkdirAll(targetDir, 0755)

	// Download and extract.
	opts.IO.Info(fmt.Sprintf("Downloading source for release %s...", releaseTag))
	tarURL := fmt.Sprintf("https://github.com/ItzEmoji/aeroflare/archive/refs/tags/%s.tar.gz", releaseTag)

	tarResp, err := http.Get(tarURL)
	if err != nil {
		return fmt.Errorf("Failed to download source: %w", err)
	}
	defer func() { _ = tarResp.Body.Close() }()
	if tarResp.StatusCode < 200 || tarResp.StatusCode >= 300 {
		return fmt.Errorf("Failed to download source: GitHub returned %s", tarResp.Status)
	}

	downloadCmd := exec.Command("tar", "-xz", "-C", targetDir, "--strip-components=1")
	downloadCmd.Stdin = tarResp.Body
	downloadCmd.Stdout = os.Stdout
	downloadCmd.Stderr = os.Stderr
	if err := downloadCmd.Run(); err != nil {
		return fmt.Errorf("Failed to download or extract source: %w", err)
	}

	proxyDir := fmt.Sprintf("%s/proxy/no-webui-native", targetDir)
	if _, err := os.Stat(proxyDir); os.IsNotExist(err) {
		return fmt.Errorf("Proxy directory %s not found in the release", proxyDir)
	}

	// Patch wrangler.toml with environment values if available.
	opts.patchWranglerToml(proxyDir)

	opts.IO.Success(fmt.Sprintf("Project scaffolded at %s", proxyDir))
	opts.IO.Info("You can now customize the worker and deploy with 'aeroflare init' or 'npx wrangler deploy'.")

	return nil
}

// patchWranglerToml applies configuration values from environment variables
// to the scaffolded wrangler.toml template.
func (opts *Options) patchWranglerToml(proxyDir string) {
	registry, repository := "", ""
	if r := os.Getenv("AEROFLARE_REGISTRY"); r != "" {
		registry = r
	} else if r := os.Getenv("NIXCACHE_REGISTRY"); r != "" {
		registry = r
	}

	if c := os.Getenv("AEROFLARE_CACHE"); c != "" {
		repository = c
	} else if c := os.Getenv("NIXCACHE_REPO"); c != "" {
		repository = c
	}

	// Try from the network package if env vars are set.
	if registry == "" || repository == "" {
		r, repo := oci.GetRegistryAndRepository()
		if registry == "" {
			registry = r
		}
		if repository == "" {
			repository = repo
		}
	}

	wranglerPath := fmt.Sprintf("%s/wrangler.toml", proxyDir)
	content, err := os.ReadFile(wranglerPath)
	if err != nil {
		opts.IO.Warning(fmt.Sprintf("Could not read wrangler.toml: %v", err))
		return
	}

	// Set the two vars the worker reads, replacing either a commented placeholder
	// or a concrete default line so it works whatever the template ships with.
	s := string(content)
	if repository != "" {
		s = setWranglerVar(s, "NIXCACHE_REPO", repository)
	}
	if registry != "" {
		s = setWranglerVar(s, "NIXCACHE_REGISTRY_URL", workerRegistryURL(registry))
	}

	_ = os.WriteFile(wranglerPath, []byte(s), 0644)
}

// setWranglerVar sets `key = "value"` in a wrangler.toml body, replacing an
// existing (possibly commented) assignment for key if present, else appending.
func setWranglerVar(body, key, value string) string {
	line := fmt.Sprintf(`%s = "%s"`, key, value)
	re := regexp.MustCompile(`(?m)^#?\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	if re.MatchString(body) {
		return re.ReplaceAllString(body, line)
	}
	return strings.TrimRight(body, "\n") + "\n" + line + "\n"
}

// workerRegistryURL turns a registry host (e.g. "ghcr.io") into the base URL the
// worker expects for NIXCACHE_REGISTRY_URL (e.g. "https://ghcr.io"); the worker
// appends the spec's "/v2" prefix itself.
func workerRegistryURL(registry string) string {
	if strings.HasPrefix(registry, "http://") || strings.HasPrefix(registry, "https://") {
		return strings.TrimRight(registry, "/")
	}
	return "https://" + strings.TrimRight(registry, "/")
}
