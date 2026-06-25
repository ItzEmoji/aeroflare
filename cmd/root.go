package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const GithubOAuthClientID = "Ov23liIJyLpd2Cse5gne" // Replace with your actual GitHub OAuth Client ID

var rootCmd = &cobra.Command{
	Use:   "aeroflare",
	Short: "A high-performance OCI-backed Nix binary cache proxy and toolkit",
	Long: `A high-performance OCI-backed Nix binary cache proxy and toolkit.

Aeroflare allows you to seamlessly cache Nix binaries into an OCI registry
(like GitHub Packages), speeding up your CI/CD pipelines and local builds.
Use it as a proxy cache, or push/pull blobs directly to/from the registry.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		PrintError(err.Error())
		os.Exit(1)
	}
}

func init() {
	// Root command flags can be added here
}

func getGithubToken() string {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return token
}

func getGithubTokenViaDeviceFlow() string {
	// Step 1: Request device code
	reqBody := strings.NewReader(fmt.Sprintf("client_id=%s&scope=repo", GithubOAuthClientID))
	req, err := http.NewRequest("POST", "https://github.com/login/device/code", reqBody)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationUri string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return ""
	}

	PrintInfo(fmt.Sprintf("\nTo authenticate with GitHub, please open your browser to: %s", deviceResp.VerificationUri))
	PrintInfo(fmt.Sprintf("And enter the code: %s\n", deviceResp.UserCode))
	PrintInfo("Waiting for authorization...")

	// Step 2: Poll for access token
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	for {
		time.Sleep(interval)
		tokenReqBody := strings.NewReader(fmt.Sprintf("client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code", GithubOAuthClientID, deviceResp.DeviceCode))
		tokenReq, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", tokenReqBody)
		if err != nil {
			continue
		}
		tokenReq.Header.Set("Accept", "application/json")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			continue
		}

		var tokenResult struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		json.NewDecoder(tokenResp.Body).Decode(&tokenResult)
		tokenResp.Body.Close()

		if tokenResult.AccessToken != "" {
			PrintSuccess("GitHub authentication successful!")
			return tokenResult.AccessToken
		}

		if tokenResult.Error != "authorization_pending" && tokenResult.Error != "slow_down" {
			PrintError(fmt.Sprintf("GitHub OAuth error: %s", tokenResult.Error))
			return ""
		}
	}
}
