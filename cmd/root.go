package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	network "aeroflare/src"
	"aeroflare/src/secrets"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

var VerboseCount int
var cacheURL string
var IsNewConfig bool

var rootCmd = &cobra.Command{
	Use:   "aeroflare",
	Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
	Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		network.DebugLogger = (VerboseCount >= 2)
		cacheURL = GetCacheURL()
	},
}

func initConfig() {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = os.Getenv("HOME")
		}
		if homeDir == "" {
			PrintError("Could not determine home directory")
			os.Exit(1)
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	aeroDir := filepath.Join(configDir, "aeroflare")
	
	if err := os.MkdirAll(aeroDir, 0755); err != nil {
		PrintError("Could not create config directory: " + err.Error())
	} else {
		configFile := filepath.Join(aeroDir, "aeroflare.yaml")
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			IsNewConfig = true
			defaultConfig := []byte(`# Aeroflare Configuration
# theme: catppuccin
# cache-url: oci://docker.io/my-org/my-cache
# backend: r2
`)
			if err := os.WriteFile(configFile, defaultConfig, 0644); err != nil {
				PrintError("Could not write default config file: " + err.Error())
			}
		}
		viper.SetConfigFile(configFile)
	}
	viper.SetEnvPrefix("AEROFLARE")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindEnv("cache", "AEROFLARE_CACHE")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			PrintError("Error reading config file: " + err.Error())
		}
	}
}

func GetCacheURL() string {
	if url := viper.GetString("cache-url"); url != "" {
		return url
	}
	if cache := viper.GetString("cache"); cache != "" {
		return "ghcr.io/" + cache
	}
	return ""
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		PrintError(err.Error())
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().CountVarP(&VerboseCount, "verbose", "v", "Enable verbose output (-v for packages, -vv for requests)")
	rootCmd.PersistentFlags().StringVar(&cacheURL, "cache-url", "", "OCI registry URL for the cache")
	viper.BindPFlag("cache-url", rootCmd.PersistentFlags().Lookup("cache-url"))
}

func getGithubToken() string {
	manager := secrets.NewManager()
	val, err := manager.Get("github-token")
	if err == nil && val != "" {
		return val
	} else if err != nil && err != keyring.ErrNotFound && !errors.Is(err, os.ErrNotExist) {
		PrintError("Warning: failed to read github-token from secret manager: " + err.Error())
	}
	
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return token
}
