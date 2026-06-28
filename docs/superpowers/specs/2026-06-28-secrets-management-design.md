# Secrets Management Design

## 1. Overview
Aeroflare needs a robust secrets management solution to handle sensitive data like GitHub tokens (for OCI registry access) and Cloudflare API tokens (for infrastructure setup). This document outlines the architecture for storing secrets securely in the OS keychain with a fallback to a restrictive plaintext file, and exposing these capabilities via the CLI.

## 2. Storage Architecture (`src/secrets`)
We will introduce a new `secrets` package containing a `Manager` interface.

### 2.1 Backend 1: OS Keychain (Primary)
*   **Library:** `github.com/zalando/go-keyring`
*   **Service Name:** `aeroflare`
*   **Behavior:** The `Manager` will first attempt to read or write the secret to the native OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service).

### 2.2 Backend 2: Plaintext File (Fallback)
*   **Path:** `~/.config/aeroflare/secrets.json`
*   **Permissions:** `0600` (readable/writable only by the owner).
*   **Behavior:** If the OS keychain is unavailable (e.g., headless Linux environment without DBus), the `Manager` automatically catches the system error and falls back to storing the key-value pair in this JSON file.

### 2.3 Interface
```go
type Manager interface {
    Set(key, value string) error
    Get(key string) (string, error)
}
```

## 3. CLI Commands (`cmd/auth.go`)
We will introduce a new `auth` command to the CLI.

### 3.1 `aeroflare auth`
*   **Behavior:** When run without sub-commands, it acts as an interactive wizard (using `charmbracelet/huh` or existing UI tools in the project) to prompt the user for common secrets:
    *   GitHub Token (Stored as `github-token`)
    *   Cloudflare API Token (Stored as `cf-token`)
*   **Flags:** `--github-token` and `--cf-token` are provided to allow non-interactive usage (e.g., in CI pipelines).

### 3.2 `aeroflare auth set <key> <value>`
*   **Behavior:** A generic key-value setter allowing the user to store arbitrary secrets in the keychain/fallback file.

## 4. Integration
The stored secrets must be seamlessly integrated into existing commands.

### 4.1 `cmd/root.go`
*   Modify `getGithubToken()` to instantiate the `secrets.Manager` and request `github-token`.
*   If the token is found, return it.
*   If not found, fall back to checking the `GITHUB_TOKEN` and `GH_TOKEN` environment variables to preserve existing behavior.

### 4.2 `cmd/init.go`
*   When `setup.RunWizard()` is executed, it will check the `secrets.Manager` for `cf-token`.
*   If the token exists, the interactive wizard can skip prompting the user for it, or simply use it for the API calls during infrastructure provisioning.
