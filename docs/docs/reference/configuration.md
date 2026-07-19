---
sidebar_position: 2
title: Configuration
---

# Configuration

Aeroflare uses [Viper](https://github.com/spf13/viper) for configuration management. It supports configuration via a YAML file, environment variables, and CLI flags.

## Configuration File

The default configuration file is located at `~/.config/aeroflare/aeroflare.yaml`.

You can interactively configure these settings by running:
```bash
aeroflare settings
```

### Schema

| Key | Description | Default |
|-----|-------------|---------|
| `theme` | UI appearance theme (`dracula`, `catppuccin`, `gruvbox-dark`, `gruvbox-light`, `default`). | `default` |
| `git-provider` | The primary Git provider backend (`github`, `gitlab`, `none`). | `none` |
| `git-token` | The authentication token for the selected Git provider. | |
| `cloudflare-api-token` | API token for interacting with Cloudflare Workers. | |
| `cache-url` | Custom OCI registry URL when not using a primary provider. | |

## Secrets Management

While basic configuration is stored in plaintext YAML, sensitive authentication credentials are by default managed securely using the operating system's native keychain or secrets manager.

When you run `aeroflare auth login` or `aeroflare init`, tokens are stored securely and retrieved dynamically at runtime.

### Credential Keys

The following keys are stored in the secrets manager:
* `github-token`
* `gitlab-token`
* `cf-token`
* `cf-account-id`

## Environment Variables

Every flag and configuration key can be overridden using environment variables prefixed with `AEROFLARE_`.

Examples:
* `AEROFLARE_CACHE_URL` overrides `--cache-url`.
* `AEROFLARE_GITHUB_TOKEN` overrides `--github-token`.
* `AEROFLARE_CF_TOKEN` overrides `--cf-token`.
