package cmd

import (
	"os"
	"path/filepath"

	network "aeroflare/src"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VerboseCount int
var cacheURL string

var rootCmd = &cobra.Command{
	Use:   "aeroflare",
	Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
	Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		network.DebugLogger = (VerboseCount >= 2)
	},
}

func initConfig() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.Getenv("HOME") + "/.config"
	}
	aeroDir := filepath.Join(configDir, "aeroflare")
	
	if err := os.MkdirAll(aeroDir, 0755); err != nil {
		PrintError("Could not create config directory: " + err.Error())
	}

	configFile := filepath.Join(aeroDir, "aeroflare.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := []byte(`# Aeroflare Configuration
# theme: catppuccin
# cache-url: oci://docker.io/my-org/my-cache
# backend: r2
`)
		os.WriteFile(configFile, defaultConfig, 0644)
	}

	viper.SetConfigFile(configFile)
	viper.SetEnvPrefix("AEROFLARE")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			PrintError("Error reading config file: " + err.Error())
		}
	}
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
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return token
}
