---
id: auth
title: Authentication Commands
sidebar_position: 2
---

# Authentication Commands

The `aeroflare auth` suite manages secrets and credentials for internal operations. Rather than just a generic credential store, the underlying logic is deeply tied to Aeroflare's specific API requirements, enforcing necessary OAuth scopes and directly setting environment variables utilized by underlying engines (e.g., OCI registry pushes).

## `aeroflare auth login`

Initiates the interactive authentication wizard. Behind the scenes, it utilizes `SecretsManager.Set` to persist credentials to the local keychain.

**Internal Keys Managed:**
*   `github-token`: Stored for GitHub operations and `ghcr.io` OCI registry interactions.
*   `gitlab-token`: Stored for GitLab operations.
*   `cf-token`: Cloudflare API Token.
*   `cf-account-id`: Cloudflare Account ID.
*   `oci-{registry}-username` / `oci-{registry}-token`: Custom OCI registry credentials.

**GitHub Device Flow Details:**
If the "Device Auth Flow" is selected for GitHub, the CLI invokes `auth.RequestDeviceCode` and `auth.PollAccessToken` using the hardcoded GitHub Client ID `Ov23liIJyLpd2Cse5gne`.

**Non-Interactive Execution:**
If specific CLI flags or environment variables are provided (mapped internally to `globalGithubToken`, `globalGitlabToken`, `globalCfToken`, `globalCfAccountID`), the command bypasses the interactive wizard, sets the provided keys directly in the `SecretsManager`, and exits.

## `aeroflare auth list`

Lists all saved authentication credentials in a tabular format, or as JSON if the `--json` flag is provided.

**Validation Mechanics:**
During execution, the CLI proactively validates tokens against remote APIs:
*   **GitHub (`github-token`)**: Invokes `GET https://api.github.com/user` with the saved token. It explicitly checks the `X-OAuth-Scopes` response header for the `write:packages` and `workflow` scopes. If missing, a warning is emitted to the user, as these are mandatory for GHCR pushes and Actions integration.
*   **GitLab (`gitlab-token`)**: Invokes `GET https://gitlab.com/api/v4/user` to resolve and display the associated GitLab username.
*   **OCI Registries**: Parses the internal keychain for keys matching the `oci-*-username` and `oci-*-token` patterns, grouping them by registry domain for display.

## `aeroflare auth import`

A utility to scrape and import active credentials from other developer tools.

**Import Sources & Mechanics:**
1.  **GitHub CLI (`gh`)**: Executes `gh auth token` as a subprocess. If successful, checks the token for the `write:packages` scope via the GitHub API and stores it as `github-token`.
2.  **GitLab CLI (`glab`)**: Executes `glab auth token` as a subprocess and stores it as `gitlab-token`.
3.  **Docker CLI**: Reads and parses `~/.docker/config.json`. It base64-decodes the `auth` field for each configured registry, splits the result into username and token, and stores them in the `SecretsManager` using the `oci-{registry}-username` and `oci-{registry}-token` formats.

## `aeroflare auth set` / `aeroflare auth remove`

Low-level utilities for direct manipulation of the `SecretsManager`.

*   **`set [key] [value]`**: Directly invokes `SecretsManager.Set(key, value)`. Useful for injecting custom secrets or overriding specific OCI registry configurations.
*   **`remove [key]`**: Directly invokes `SecretsManager.Delete(key)`. 

## Credential Resolution & Environment Injection

Commands across the Aeroflare CLI that require authentication rely on the `pkg/cmd/auth/shared/shared.go` mechanisms. These functions handle fallback chains and environment injection for subprocesses.

**Resolution Chain (`RequireGithubToken`, `RequireCloudflareToken`, etc.):**
1.  **Global Flags**: Checks variables set by command-line flags (e.g., `globalGithubToken`).
2.  **Keychain**: Uses `auth.NewResolver().WithSecretsManager()` to retrieve the secret from the internal store.
3.  **Environment Variables**: As a fallback, resolvers inspect standard environment variables (e.g., `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, `GITHUB_TOKEN`, `GITLAB_TOKEN`).
4.  **Interactive Prompt**: If all the above fail and `os.Stdin` is a TTY (`isTerminal() == true`), the respective interactive wizard (from `pkg/cmd/auth/shared/wizard.go`) is launched dynamically.
5.  **Fatal Exit**: If not in a TTY, the CLI prints an error and exits with code 1.

**Environment Injection (`getTokenForRegistry`, `getOptionalTokenForRegistry`):**
When resolving tokens for OCI registry pushes, the CLI manipulates the current process's environment variables to ensure underlying engines authenticate successfully:
*   For `ghcr.io`: Exports `oci_token` and `GITHUB_TOKEN` into the environment.
*   For other registries: Exports `oci_token` into the environment.
