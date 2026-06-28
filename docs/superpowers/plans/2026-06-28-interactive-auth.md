# Interactive Auth Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a fully interactive CLI wizard for authenticating with GitHub, Cloudflare, and custom OCI registries.

**Architecture:** We use `charmbracelet/huh` to render interactive forms. The GitHub device auth flow will be implemented purely via standard HTTP requests in a new package `src/auth`. The wizard logic itself lives in `cmd/auth_wizard.go` and is called by `cmd/auth.go` if no non-interactive flags are provided.

**Tech Stack:** Go, `github.com/charmbracelet/huh`

## Global Constraints

- Must save GitHub tokens as `github-token`.
- Must save Cloudflare credentials as `cf-token` and `cf-user-id`.
- Must save OCI registry credentials as `oci-<registry>-username` and `oci-<registry>-token`.
- GitHub Device Auth must use Client ID `Ov23liIJyLpd2Cse5gne`.

---

### Task 1: Add Dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: N/A
- Produces: Added `github.com/charmbracelet/huh` to the module.

- [ ] **Step 1: Write minimal implementation**

```bash
go get github.com/charmbracelet/huh
go mod tidy
```

- [ ] **Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add charmbracelet/huh dependency"
```

---

### Task 2: Implement GitHub Device Auth Flow

**Files:**
- Create: `src/auth/github.go`
- Create: `src/auth/github_test.go`

**Interfaces:**
- Consumes: Client ID `Ov23liIJyLpd2Cse5gne`
- Produces: 
  - `type DeviceCodeResponse struct { DeviceCode string, UserCode string, VerificationURI string, Interval int }`
  - `func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error)`
  - `func PollAccessToken(clientID string, deviceCode string, interval int) (string, error)`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestDeviceCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"device_code": "dc123", "user_code": "123-456", "verification_uri": "https://github.com/login/device", "interval": 5}`))
	}))
	defer ts.Close()

	// Temporarily override the base URL for testing (we'll add a variable for this in implementation)
	originalURL := githubBaseURL
	githubBaseURL = ts.URL
	defer func() { githubBaseURL = originalURL }()

	res, err := RequestDeviceCode("test-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UserCode != "123-456" {
		t.Errorf("expected 123-456, got %s", res.UserCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./src/auth`
Expected: FAIL (package / functions not defined)

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var githubBaseURL = "https://github.com"

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	reqBody := []byte(fmt.Sprintf(`{"client_id":"%s"}`, clientID))
	req, err := http.NewRequest("POST", githubBaseURL+"/login/device/code", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

func PollAccessToken(clientID, deviceCode string, interval int) (string, error) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		reqBody := []byte(fmt.Sprintf(`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`, clientID, deviceCode))
		req, _ := http.NewRequest("POST", githubBaseURL+"/login/oauth/access_token", bytes.NewBuffer(reqBody))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue // retry on network error
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result TokenResponse
		json.Unmarshal(body, &result)

		if result.AccessToken != "" {
			return result.AccessToken, nil
		}

		if result.Error == "authorization_pending" || result.Error == "slow_down" {
			continue
		}
		
		if result.Error != "" {
			return "", errors.New(result.Error)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./src/auth`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/auth/github.go src/auth/github_test.go
git commit -m "feat(auth): implement GitHub device authorization flow"
```

---

### Task 3: Implement Interactive Auth Wizard

**Files:**
- Create: `cmd/auth_wizard.go`
- Modify: `cmd/auth.go`

**Interfaces:**
- Consumes: `auth.RequestDeviceCode`, `auth.PollAccessToken`, `secrets.Manager`
- Produces: `func runInteractiveAuth()` in `cmd` package.

- [ ] **Step 1: Write minimal implementation for `cmd/auth_wizard.go`**

```go
package cmd

import (
	"fmt"
	"aeroflare/src/auth"
	"github.com/charmbracelet/huh"
)

const githubClientID = "Ov23liIJyLpd2Cse5gne"

func runInteractiveAuth() {
	var service string
	
	err := huh.NewSelect[string]().
		Title("What do you want to authenticate?").
		Options(
			huh.NewOption("GitHub / GitLab", "github"),
			huh.NewOption("Cloudflare", "cloudflare"),
			huh.NewOption("Custom OCI Registry", "oci"),
		).
		Value(&service).
		Run()
		
	if err != nil {
		PrintError("Cancelled")
		return
	}

	manager := getSecretsManager()

	switch service {
	case "github":
		var ghMethod string
		err = huh.NewSelect[string]().
			Title("How would you like to authenticate?").
			Options(
				huh.NewOption("Device Auth Flow (Browser)", "device"),
				huh.NewOption("Enter Token Manually", "manual"),
			).
			Value(&ghMethod).
			Run()
		if err != nil {
			return
		}

		var token string
		if ghMethod == "device" {
			fmt.Println("Requesting device code...")
			res, err := auth.RequestDeviceCode(githubClientID)
			if err != nil {
				PrintError(fmt.Sprintf("Failed to request code: %v", err))
				return
			}
			fmt.Printf("Please go to %s and enter the code: %s\n", res.VerificationURI, res.UserCode)
			fmt.Println("Waiting for authorization...")
			
			token, err = auth.PollAccessToken(githubClientID, res.DeviceCode, res.Interval)
			if err != nil {
				PrintError(fmt.Sprintf("Authorization failed: %v", err))
				return
			}
		} else {
			huh.NewInput().Title("GitHub / GitLab Token").EchoMode(huh.EchoModePassword).Value(&token).Run()
		}
		
		if token != "" {
			if err := manager.Set("github-token", token); err != nil {
				PrintError(fmt.Sprintf("Failed to save token: %v", err))
				return
			}
			fmt.Println("Success! Token saved. This will automatically be used for GitHub APIs and the ghcr.io container registry.")
		}

	case "cloudflare":
		var apiToken, userID string
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Cloudflare API Token").EchoMode(huh.EchoModePassword).Value(&apiToken),
				huh.NewInput().Title("Cloudflare User ID").Value(&userID),
			),
		).Run()

		if apiToken != "" {
			manager.Set("cf-token", apiToken)
		}
		if userID != "" {
			manager.Set("cf-user-id", userID)
		}
		fmt.Println("Cloudflare credentials saved.")

	case "oci":
		var registry, user, pass string
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Registry URL (e.g. registry.gitlab.com)").Value(&registry),
				huh.NewInput().Title("Username").Value(&user),
				huh.NewInput().Title("Token / Password").EchoMode(huh.EchoModePassword).Value(&pass),
			),
		).Run()

		if registry != "" {
			manager.Set(fmt.Sprintf("oci-%s-username", registry), user)
			manager.Set(fmt.Sprintf("oci-%s-token", registry), pass)
			fmt.Println("OCI credentials saved.")
		}
	}
}
```

- [ ] **Step 2: Modify `cmd/auth.go` to use the wizard**

Modify `cmd/auth.go` inside the `Run` function for `authCmd`:

```go
		if githubToken == "" && cfToken == "" {
			runInteractiveAuth()
			return
		}
```
Replace the previous stub message `fmt.Println("Interactive mode not fully implemented in CLI yet, please use flags.")`.

- [ ] **Step 3: Test compilation**

Run: `go build ./cmd/...`
Expected: Passes without errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/auth.go cmd/auth_wizard.go
git commit -m "feat(auth): implement interactive setup wizard via huh"
```
