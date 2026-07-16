---
sidebar_position: 3
title: Running the Proxy Server
---

# Running the Proxy Server

The Aeroflare proxy server acts as a standard HTTP binary cache. It intercepts requests from the Nix daemon and translates them into OCI manifest fetches and layer pulls.

## Starting the Proxy

To start the proxy daemon manually, run:

```bash
nix run github:ItzEmoji/aeroflare -- proxy
```

By default, the proxy listens on `http://127.0.0.1:8080`.

## Configuring Nix

To tell Nix to use your local Aeroflare proxy, you must pass it as a substituter.

### Ad-hoc Usage

For a single command, you can pass the flag directly to Nix:

```bash
nix build .#default --option extra-substituters "http://127.0.0.1:8080"
```

### Persistent Configuration

To configure Nix to always use the proxy, edit your `nix.conf` (usually located at `~/.config/nix/nix.conf` or `/etc/nix/nix.conf`):

```ini
extra-substituters = http://127.0.0.1:8080
```

*Note: You must ensure the Aeroflare proxy is running in the background whenever Nix attempts to build or fetch packages, otherwise substitution will fail.*

## Using the `run` Wrapper (Recommended)

Managing the proxy daemon manually can be tedious. The recommended approach for local development is to use the `aeroflare run` wrapper.

> **Important:** Currently, if you want Aeroflare to successfully push the resulting artifacts, you must pass the `--print-out-paths` flag to your Nix build command so Aeroflare knows what to upload.

```bash
nix run github:ItzEmoji/aeroflare -- run -- nix build .#default --print-out-paths
```


This command automatically:
1. Spawns an ephemeral proxy server on a random open port.
2. Appends the `--option extra-substituters` flag to the inner Nix command.
3. Shuts down the proxy when the build finishes.
4. **Pushes** any newly generated build artifacts to the remote backend automatically.

## Running as a Container

If you'd rather not install Nix or Go just to run the proxy, it's also
published as a container image — see [Docker](./docker.md).
