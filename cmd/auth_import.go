package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var authImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import credentials from other CLIs (gh, glab, docker)",
	RunE: func(cmd *cobra.Command, args []string) error {
		manager := getSecretsManager()
		imported := 0

		// 1. GitHub CLI
		if ghPath, err := exec.LookPath("gh"); err == nil {
			out, err := exec.Command(ghPath, "auth", "token").Output()
			if err == nil {
				token := strings.TrimSpace(string(out))
				if token != "" {
					if err := manager.Set("github-token", token); err == nil {
						fmt.Println("✅ Imported GitHub token from gh CLI")
						imported++
					}
				}
			}
		}

		// 2. GitLab CLI
		if glabPath, err := exec.LookPath("glab"); err == nil {
			out, err := exec.Command(glabPath, "auth", "token").Output()
			if err == nil {
				token := strings.TrimSpace(string(out))
				if token != "" {
					if err := manager.Set("gitlab-token", token); err == nil {
						fmt.Println("✅ Imported GitLab token from glab CLI")
						imported++
					}
				}
			}
		}

		// 3. Docker CLI
		homeDir, err := os.UserHomeDir()
		if err == nil {
			dockerConfigPath := filepath.Join(homeDir, ".docker", "config.json")
			if data, err := os.ReadFile(dockerConfigPath); err == nil {
				var config struct {
					Auths map[string]struct {
						Auth string `json:"auth"`
					} `json:"auths"`
				}
				if err := json.Unmarshal(data, &config); err == nil {
					for registry, authData := range config.Auths {
						if authData.Auth == "" {
							continue
						}
						decoded, err := base64.StdEncoding.DecodeString(authData.Auth)
						if err != nil {
							continue
						}
						parts := strings.SplitN(string(decoded), ":", 2)
						if len(parts) == 2 {
							username := parts[0]
							token := parts[1]

							err1 := manager.Set(fmt.Sprintf("oci-%s-username", registry), username)
							err2 := manager.Set(fmt.Sprintf("oci-%s-token", registry), token)

							if err1 == nil && err2 == nil {
								fmt.Printf("✅ Imported OCI credentials for %s from Docker config\n", registry)
								imported++
							}
						}
					}
				}
			}
		}

		if imported == 0 {
			fmt.Println("No credentials found to import.")
		} else {
			fmt.Printf("Successfully imported %d credential(s).\n", imported)
		}

		return nil
	},
}

func init() {
	authCmd.AddCommand(authImportCmd)
}
