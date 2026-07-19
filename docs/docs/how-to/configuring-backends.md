---
sidebar_position: 1
title: Configuring Storage
---

# Configuring Storage

Aeroflare stores your Nix binaries in an **OCI Registry** (such as GitHub
Container Registry). Each Nix package is published as its own OCI image, tagged
with its store hash, and its `.narinfo` metadata is carried in the image's
annotations. This "native OCI" layout is fully stateless: there is no central
index to maintain, and lookups resolve directly to a single manifest request.

## Initial Setup
To provision resources and configure a cache for the first time, run the interactive onboarding wizard:
```bash
nix run github:ItzEmoji/aeroflare -- init
```

---

## Configuring an Existing Cache

To change the Nix public signing key for an existing cache, use the `configure`
command:
```bash
nix run github:ItzEmoji/aeroflare -- configure
```
This updates the cache metadata (OCI manifest annotations) directly in the registry.

---

## Client Settings and Credentials

To configure registry logins on your local machine, you can use the interactive
authentication commands or environment variables.

### Interactive CLI Authentication
Run the login command to configure credentials interactively:
```bash
nix run github:ItzEmoji/aeroflare -- auth login
```
This wizard securely stores tokens (GitHub, GitLab, or Cloudflare API tokens) in your local credential store.

You can also manage credentials manually:
- **Set a token**: `nix run github:ItzEmoji/aeroflare -- auth set [key] [value]` (e.g. `cf-token`, `github-token`, `gitlab-token`, `cf-account-id`)
- **List stored keys**: `nix run github:ItzEmoji/aeroflare -- auth list`
- **Remove a credential**: `nix run github:ItzEmoji/aeroflare -- auth remove [key]`
- **Import credentials from other CLIs**: `nix run github:ItzEmoji/aeroflare -- auth import` (automatically detects and imports active credentials from Docker, the GitHub CLI `gh`, or the GitLab CLI `glab`).

### Environment Variables
Alternatively, you can configure credentials directly via environment variables:

| Variable | Description |
| :--- | :--- |
| `GITHUB_TOKEN` / `GH_TOKEN` | Authentication token for GitHub Packages (ghcr.io) |
| `GITLAB_TOKEN` | Authentication token for GitLab Registry |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare Account ID |
