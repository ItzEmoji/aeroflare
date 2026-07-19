---
id: core
title: Core Commands
sidebar_position: 1
---
# Core Commands

This document covers the internal implementation mechanics of the core commands and global state management for the Aeroflare CLI, implemented across `pkg/cmd/root/root.go`, `pkg/cmd/init/init.go`, `pkg/cmd/configure/configure.go`, and `pkg/cmd/settings/settings.go`.

## Global Flags and Environment State (`pkg/cmd/root/root.go`)

The `aeroflare` root command uses Viper to manage global state and flag bindings:

- `--verbose` (`-v`, `-vv`): Controls the `network.DebugLogger` boolean. `-vv` triggers request-level logging (`VerboseCount >= 2`).
- `--cache-url`, `--github-token`, `--gitlab-token`, `--cf-token`, `--cf-account-id`: Core configuration flags.

Viper automatically maps all flags to environment variables by applying the `AEROFLARE_` prefix and replacing dashes with underscores (e.g., `--cache-url` maps to `AEROFLARE_CACHE_URL`).

Additionally, `pkg/cmd/root/root.go` binds a specific alias, `AEROFLARE_CACHE`. When this environment variable is used instead of `AEROFLARE_CACHE_URL`, the `GetCacheURL()` function automatically prefixes the provided cache name with `ghcr.io/` (e.g., `AEROFLARE_CACHE=my-org/my-cache` becomes `ghcr.io/my-org/my-cache`).

## aeroflare init (`pkg/cmd/init/init.go`)

The `init` command orchestrates the `setup.RunWizard()` and `setup.RunProvision(cfg)` flow. 

Crucially, before invoking the provisioner, `init.go` dynamically mutates the process environment by exporting required tokens so that underlying SDK calls can authenticate:
- It always exports `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` (needed to deploy the Worker).
- If GitHub Registry (`ghcr.io`) or GitHub Git Provider is selected, it exports `GITHUB_TOKEN`.

## aeroflare configure (`pkg/cmd/configure/configure.go`)

The `configure` command reads and writes cache configuration directly to OCI manifest annotations using `network.PushConfigManifest`.

It manipulates the following specific OCI annotation key:
- `aeroflare.public-key`: The nix cache public key.

## aeroflare settings (`pkg/cmd/settings/settings.go`)

The `settings` command provides an interactive UI, but under the hood, it directly manipulates the Viper configuration store and flushes the changes to disk (`viper.WriteConfig()`).

Depending on the user's choices in the `huh` forms, it mutates the following internal Viper configuration keys:
- `theme`: UI color scheme (e.g., `dracula`, `catppuccin`, `gruvbox-dark`).
- `git-provider`: Set to `github`, `gitlab`, or `none`.
- `git-token`: Stores the corresponding GitHub or GitLab token.
- `cloudflare-api-token`: Stores the Cloudflare token used to deploy the Worker.
- `cache-url`: Stores the custom OCI registry URL.
