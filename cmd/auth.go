package cmd

import (
	"fmt"
	"aeroflare/src/secrets"
	"github.com/spf13/cobra"
)

var (
	githubToken string
	cfToken     string
)

// SecretsManager allows mocking in tests
var SecretsManager secrets.Manager

func getSecretsManager() secrets.Manager {
	if SecretsManager != nil {
		return SecretsManager
	}
	return secrets.NewManager()
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage Aeroflare authentication secrets",
	Run: func(cmd *cobra.Command, args []string) {
		manager := getSecretsManager()
		
		if githubToken != "" {
			err := manager.Set("github-token", githubToken)
			if err != nil {
				PrintError(err.Error())
				return
			}
			fmt.Println("Saved github-token")
		}
		
		if cfToken != "" {
			err := manager.Set("cf-token", cfToken)
			if err != nil {
				PrintError(err.Error())
				return
			}
			fmt.Println("Saved cf-token")
		}
		
		if githubToken == "" && cfToken == "" {
			runInteractiveAuth()
			return
		}
	},
}

var authSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set an arbitrary secret",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		manager := getSecretsManager()
		err := manager.Set(args[0], args[1])
		if err != nil {
			PrintError(err.Error())
			return
		}
		fmt.Printf("Saved %s\n", args[0])
	},
}

func init() {
	authCmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub Token")
	authCmd.Flags().StringVar(&cfToken, "cf-token", "", "Cloudflare Token")
	
	authCmd.AddCommand(authSetCmd)
	rootCmd.AddCommand(authCmd)
}
