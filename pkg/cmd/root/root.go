package root

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itzemoji/aeroflare/internal/oci"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth"
	"github.com/itzemoji/aeroflare/pkg/cmd/blob"
	"github.com/itzemoji/aeroflare/pkg/cmd/configure"
	initcmd "github.com/itzemoji/aeroflare/pkg/cmd/init"
	"github.com/itzemoji/aeroflare/pkg/cmd/prepare"
	"github.com/itzemoji/aeroflare/pkg/cmd/proxy"
	"github.com/itzemoji/aeroflare/pkg/cmd/push"
	"github.com/itzemoji/aeroflare/pkg/cmd/run"
	"github.com/itzemoji/aeroflare/pkg/cmd/scaffold"
	"github.com/itzemoji/aeroflare/pkg/cmd/settings"
	"github.com/itzemoji/aeroflare/pkg/cmd/version"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultConfig = `# Aeroflare Configuration
# theme: catppuccin
# cache-url: oci://docker.io/my-org/my-cache
`

// InitConfig locates (or creates) aeroflare.yaml under XDG_CONFIG_HOME (or
// ~/.config) and returns a Viper bound to it, along with whether the file was
// created fresh on this run. The AEROFLARE_* environment prefix is registered
// on the returned Viper.
func InitConfig() (*viper.Viper, bool, error) {
	v := viper.New()

	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.Getenv("HOME")
		}
		if homeDir == "" {
			return nil, false, fmt.Errorf("Could not determine home directory")
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	aeroDir := filepath.Join(configDir, "aeroflare")

	isNew := false
	if err := os.MkdirAll(aeroDir, 0755); err != nil {
		return nil, false, fmt.Errorf("Could not create config directory: %w", err)
	}

	configFile := filepath.Join(aeroDir, "aeroflare.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		isNew = true
		if err := os.WriteFile(configFile, []byte(defaultConfig), 0644); err != nil {
			return nil, false, fmt.Errorf("Could not write default config file: %w", err)
		}
	}
	v.SetConfigFile(configFile)

	v.SetEnvPrefix("AEROFLARE")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	_ = v.BindEnv("cache", "AEROFLARE_CACHE")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, isNew, fmt.Errorf("Error reading config file: %w", err)
		}
	}

	return v, isNew, nil
}

// ResolveCacheURL resolves the effective OCI cache URL: an explicit --cache-url
// flag wins, otherwise a shorthand --cache "org/repo" value is expanded to a
// ghcr.io URL. Returns "" if neither is set.
func ResolveCacheURL(v *viper.Viper) string {
	if url := v.GetString("cache-url"); url != "" {
		return url
	}
	if cache := v.GetString("cache"); cache != "" {
		return "ghcr.io/" + cache
	}
	return ""
}

// NewCmdRoot builds the aeroflare root command and the full subcommand tree.
// This is the one place the tree is assembled.
func NewCmdRoot(f *cmdutil.Factory, buildVersion, buildDate string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aeroflare",
		Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
		Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			oci.DebugLogger = f.Overrides.Verbose >= 2

			// Bind --cache-url to viper here, at Execute time, rather than
			// while building the command. Building the command must not
			// force config-file I/O (e.g. doc generation builds the root
			// command purely to walk the tree, and should not create a
			// user's config file as a side effect).
			if v, err := f.Config(); err == nil {
				_ = v.BindPFlag("cache-url", cmd.PersistentFlags().Lookup("cache-url"))
			}

			return nil
		},
	}

	cmd.PersistentFlags().CountVarP(&f.Overrides.Verbose, "verbose", "v", "Enable verbose output (-v for packages, -vv for requests)")
	cmd.PersistentFlags().String("cache-url", "", "OCI registry URL for the cache")
	cmd.PersistentFlags().StringVar(&f.Overrides.GithubToken, "github-token", "", "GitHub Token")
	cmd.PersistentFlags().StringVar(&f.Overrides.GitlabToken, "gitlab-token", "", "GitLab Token")
	cmd.PersistentFlags().StringVar(&f.Overrides.CfToken, "cf-token", "", "Cloudflare API Token")
	cmd.PersistentFlags().StringVar(&f.Overrides.CfUserID, "cf-user-id", "", "Cloudflare Account ID")

	cmd.AddCommand(version.NewCmdVersion(f, buildVersion, buildDate))
	cmd.AddCommand(auth.NewCmdAuth(f))
	cmd.AddCommand(push.NewCmdPush(f))
	cmd.AddCommand(run.NewCmdRun(f))
	cmd.AddCommand(blob.NewCmdPushBlob(f))
	cmd.AddCommand(blob.NewCmdPullBlob(f))
	cmd.AddCommand(proxy.NewCmdProxy(f))
	cmd.AddCommand(configure.NewCmdConfigure(f))
	cmd.AddCommand(prepare.NewCmdPrepare(f))
	cmd.AddCommand(settings.NewCmdSettings(f))
	cmd.AddCommand(scaffold.NewCmdScaffold(f))
	cmd.AddCommand(initcmd.NewCmdInit(f))

	return cmd
}
