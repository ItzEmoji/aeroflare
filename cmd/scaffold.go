package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	network "aeroflare/src"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var scaffoldRelease string
var scaffoldOutputDir string

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold",
	Short: "Generate local project files for an Aeroflare worker",
	Long: `Download an Aeroflare release and scaffold a local project directory
containing the worker source, wrangler.toml, and configuration files.

This command generates local files only — it does not create or modify
any remote infrastructure. Use 'aeroflare init' for that.`,
	Run: func(cmd *cobra.Command, args []string) {
		runScaffold()
	},
}

func init() {
	scaffoldCmd.Flags().StringVar(&scaffoldRelease, "release", "", "Release tag to download (default: prompt)")
	scaffoldCmd.Flags().StringVar(&scaffoldOutputDir, "output-dir", "./aeroflare-proxy", "Directory to scaffold into")
	rootCmd.AddCommand(scaffoldCmd)
}

func runScaffold() {
	// Fetch available releases.
	PrintInfo("Fetching available releases...")
	resp, err := http.Get("https://api.github.com/repos/ItzEmoji/aeroflare/releases")
	if err != nil {
		PrintError(fmt.Sprintf("Failed to fetch releases: %v", err))
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		PrintError(fmt.Sprintf("Failed to decode releases: %v", err))
		os.Exit(1)
	}
	if len(releases) == 0 {
		PrintError("No releases found.")
		os.Exit(1)
	}

	releaseTag := scaffoldRelease
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
			os.Exit(0)
		}
	}

	targetDir := scaffoldOutputDir
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
		os.Exit(0)
	}

	if targetDir == "" {
		targetDir = "./aeroflare-proxy"
	}
	_ = os.MkdirAll(targetDir, 0755)

	// Download and extract.
	PrintInfo(fmt.Sprintf("Downloading source for release %s...", releaseTag))
	tarURL := fmt.Sprintf("https://github.com/ItzEmoji/aeroflare/archive/refs/tags/%s.tar.gz", releaseTag)

	tarResp, err := http.Get(tarURL)
	if err != nil {
		PrintError(fmt.Sprintf("Failed to download source: %v", err))
		os.Exit(1)
	}
	defer func() { _ = tarResp.Body.Close() }()
	if tarResp.StatusCode < 200 || tarResp.StatusCode >= 300 {
		PrintError(fmt.Sprintf("Failed to download source: GitHub returned %s", tarResp.Status))
		os.Exit(1)
	}

	downloadCmd := exec.Command("tar", "-xz", "-C", targetDir, "--strip-components=1")
	downloadCmd.Stdin = tarResp.Body
	downloadCmd.Stdout = os.Stdout
	downloadCmd.Stderr = os.Stderr
	if err := downloadCmd.Run(); err != nil {
		PrintError(fmt.Sprintf("Failed to download or extract source: %v", err))
		os.Exit(1)
	}

	// Select backend for wrangler.toml patching.
	var indexType string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose your cache backend").
				Options(
					huh.NewOption("Cloudflare R2", "r2"),
					huh.NewOption("Native OCI Tags", "native"),
					huh.NewOption("JSON index in OCI", "json"),
				).
				Value(&indexType),
		),
	).Run()
	if err != nil {
		os.Exit(0)
	}

	proxyDir := fmt.Sprintf("%s/proxy/no-webui-%s", targetDir, indexType)
	if _, err := os.Stat(proxyDir); os.IsNotExist(err) {
		PrintError(fmt.Sprintf("Proxy directory %s not found in the release", proxyDir))
		os.Exit(1)
	}

	// Patch wrangler.toml with environment values if available.
	patchWranglerToml(proxyDir, indexType)

	PrintSuccess(fmt.Sprintf("Project scaffolded at %s", proxyDir))
	PrintInfo("You can now customize the worker and deploy with 'aeroflare init' or 'npx wrangler deploy'.")
}

// patchWranglerToml applies configuration values from environment variables
// to the scaffolded wrangler.toml template.
func patchWranglerToml(proxyDir, indexType string) {
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
		r, repo := network.GetRegistryAndRepository()
		if registry == "" {
			registry = r
		}
		if repository == "" {
			repository = strings.TrimSuffix(repo, "/nix-cache")
		}
	}

	wranglerPath := fmt.Sprintf("%s/wrangler.toml", proxyDir)
	content, err := os.ReadFile(wranglerPath)
	if err != nil {
		PrintWarning(fmt.Sprintf("Could not read wrangler.toml: %v", err))
		return
	}

	s := string(content)
	if repository != "" {
		s = strings.Replace(s, `# NIXCACHE_REPO = "<NIXCACHE_REPO>"`, fmt.Sprintf(`NIXCACHE_REPO = "%s"`, repository), 1)
	}
	if registry != "" {
		s = strings.Replace(s, `# NIXCACHE_REGISTRY = "<NIXCACHE_REGISTRY>"`, fmt.Sprintf(`NIXCACHE_REGISTRY = "%s"`, registry), 1)
	}
	s = strings.Replace(s, `# NIXCACHE_UPSTREAM = "<NIXCACHE_UPSTREAM>"`, `NIXCACHE_UPSTREAM = "https://cache.nixos.org"`, 1)
	s = strings.Replace(s, `# NIXCACHE_INDEX_TTL = "<NIXCACHE_INDEX_TTL"`, `NIXCACHE_INDEX_TTL = "200"`, 1)

	if indexType == "r2" {
		bucket := os.Getenv("R2_BUCKET")
		if bucket == "" {
			bucket = strings.ReplaceAll(repository, "/", "-") + "-index"
		}
		s = strings.Replace(s, `# [[r2_buckets]]`, `[[r2_buckets]]`, 1)
		s = strings.Replace(s, `# binding = "BUCKET"`, `binding = "BUCKET"`, 1)
		s = strings.Replace(s, `# bucket_name = "<bucket-name>"`, fmt.Sprintf(`bucket_name = "%s"`, bucket), 1)
		s = strings.Replace(s, `# bucket_name = "<bucket-name>" `, fmt.Sprintf(`bucket_name = "%s"`, bucket), 1)
	}

	_ = os.WriteFile(wranglerPath, []byte(s), 0644)
}
