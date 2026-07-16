---
sidebar_position: 8
title: Docker
---

# Docker

The Aeroflare proxy is published as a container image, for environments
where installing Nix or Go isn't an option (or isn't wanted) just to run
the substituter.

## Prerequisites

- Docker (or any OCI-compatible container runtime).
- A cache already populated via `aeroflare push` / `aeroflare run` / the
  [GitHub Action](./github-action.md) — the container only serves an
  existing cache, it doesn't build or push anything.

## Running the container

```bash
docker run -e AEROFLARE_CACHE=<org>/<cache> -p 8080:8080 ghcr.io/itzemoji/aeroflare-proxy
```

This starts the proxy listening on `0.0.0.0:8080` inside the container,
mapped to `localhost:8080` on the host. `AEROFLARE_CACHE` takes the same
shorthand `org/repo` form as the CLI's `--cache` flag and defaults to
`ghcr.io` as the registry; see [Configuration](../reference/configuration.md)
for the full set of accepted values, including `AEROFLARE_CACHE_URL` for
registries other than `ghcr.io`.

## Pointing Nix at it

Once the container is running, point Nix at `http://localhost:8080` exactly
as you would a locally-run proxy — see
["Configuring Nix"](./running-proxy.md#configuring-nix) for ad-hoc and
persistent setup.

## Private caches

Reading a private cache requires credentials in the container's
environment. These follow the CLI's own credential resolution
(`internal/auth`), not the separate `aeroflare-ci` build tool's
`AEROFLARE_TOKEN_<HOST>` variables:

| Registry | Env var |
| --- | --- |
| `ghcr.io` | `GITHUB_TOKEN` (or `GH_TOKEN`) |
| `registry.gitlab.com` | `GITLAB_TOKEN` |
| anything else | `oci_token`, plus `AEROFLARE_GIT_USERNAME` if the registry checks the username (e.g. Docker Hub) |

```bash
docker run \
  -e AEROFLARE_CACHE=my-org/my-cache \
  -e GITHUB_TOKEN=ghp_xxx \
  -p 8080:8080 ghcr.io/itzemoji/aeroflare-proxy
```

## Running it persistently

```yaml
services:
  aeroflare-proxy:
    image: ghcr.io/itzemoji/aeroflare-proxy
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      AEROFLARE_CACHE: my-org/my-cache
      GITHUB_TOKEN: ghp_xxx
```

## Limitations

The container writes a default config file to `$HOME/.config/aeroflare/` on
first start, the same way the CLI does when run outside a container. This
requires a writable home directory, so the image does not currently support
`docker run --read-only`.
