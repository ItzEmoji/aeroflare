package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"aeroflare/src/ui"
	"github.com/spf13/cobra"
)

var authListJson bool

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved authentication credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		manager := getSecretsManager()
		keys, err := manager.List()
		if err != nil {
			if authListJson {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				PrintError(err.Error())
			}
			return err
		}
		
		if len(keys) == 0 {
			if authListJson {
				fmt.Println("[]")
			} else {
				fmt.Println("No credentials saved.")
			}
			return nil
		}
		
		// Map to group OCI tokens
		type Entry struct {
			Service string `json:"service"`
			Info    string `json:"info"`
			Key     string `json:"key"`
		}
		var entries []Entry
		ociRegistries := make(map[string]map[string]string)

		for _, key := range keys {
			val, _ := manager.Get(key)

			if key == "github-token" {
				info := "Token"
				if user := getGithubUser(val); user != "" {
					info = "Username: " + user
				}
				entries = append(entries, Entry{Service: "GitHub", Info: info, Key: key})
			} else if key == "gitlab-token" {
				info := "Token"
				if user := getGitlabUser(val); user != "" {
					info = "Username: " + user
				}
				entries = append(entries, Entry{Service: "GitLab", Info: info, Key: key})
			} else if key == "cf-token" {
				entries = append(entries, Entry{Service: "Cloudflare", Info: "API Token", Key: key})
			} else if key == "cf-user-id" {
				entries = append(entries, Entry{Service: "Cloudflare", Info: "Account ID", Key: key})
			} else if strings.HasPrefix(key, "oci-") {
				parts := strings.Split(key, "-")
				if len(parts) >= 3 {
					registry := strings.Join(parts[1:len(parts)-1], "-")
					suffix := parts[len(parts)-1]
					if _, ok := ociRegistries[registry]; !ok {
						ociRegistries[registry] = make(map[string]string)
					}
					ociRegistries[registry][suffix] = key
				} else {
					entries = append(entries, Entry{Service: "Custom", Info: "Secret", Key: key})
				}
			} else {
				entries = append(entries, Entry{Service: "Custom", Info: "Secret", Key: key})
			}
		}

		for reg, data := range ociRegistries {
			info := "Unknown"
			keyStr := ""
			
			if userKey, ok := data["username"]; ok {
				user, _ := manager.Get(userKey)
				info = "Username: " + user + " (" + reg + ")"
				keyStr = userKey
			}
			if tokenKey, ok := data["token"]; ok {
				if keyStr != "" {
					keyStr += ", " + tokenKey
				} else {
					info = "Token (" + reg + ")"
					keyStr = tokenKey
				}
			}
			
			entries = append(entries, Entry{Service: "OCI Registry", Info: info, Key: keyStr})
		}

		if authListJson {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(entries); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "✅ The list has been successfully exported to json.")
			return nil
		}

		var rows [][]string
		for _, e := range entries {
			rows = append(rows, []string{e.Service, e.Info, e.Key})
		}

		fmt.Println("Saved credentials:")
		ui.PrintTable([]string{"Service", "Info", "Key(s)"}, rows)
		return nil
	},
}

func init() {
	authListCmd.Flags().BoolVar(&authListJson, "json", false, "Export credentials list as JSON")
}

func getGithubUser(token string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return data.Login
}

func getGitlabUser(token string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest("GET", "https://gitlab.com/api/v4/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return data.Username
}
