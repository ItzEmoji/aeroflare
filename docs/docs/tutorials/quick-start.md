---
id: quick-start
title: Quick Start
sidebar_position: 1
---
Welcome to Aeroflare! This guide will get you up and running with your own lightning-fast Nix cache infrastructure in just a few minutes. 

We'll cover how to initialize your configuration, authenticate with your cache provider, run the proxy, and push your first cached build.

## 1. Install & Initialize

The fastest way to get started is to use the interactive setup wizard via Nix. This provisions your storage (a GitHub Container Registry repository and a Cloudflare Worker) and configures your local environment.

```bash
nix run github:ItzEmoji/aeroflare -- init
```

The `init` command guides you through an interactive setup:
1. It asks for integration with GitHub or GitLab.
2. It automatically creates a private repository to host your cache.
3. Finally, it deploys a serverless worker which acts as your remote proxy.

:::info Authentication

During initialization, the wizard will prompt you for the necessary credentials. If you don't have them defined in your local OS keychain or secrets manager, you'll be asked to provide:

- A **GitHub / GitLab Personal Access Token**
- A **Cloudflare API Token** (to deploy the Worker)

Aeroflare securely saves these tokens for future use.
:::

## 2. Run the Proxy

Aeroflare operates as a local proxy that intercepts Nix daemon requests. To spin up the proxy server, use:

```bash
nix run github:ItzEmoji/aeroflare -- proxy
```

> **Note:** This command runs in the foreground and will block your terminal. Please run it in the background or open a new terminal window to proceed with the next steps.

This starts the local proxy server, ready to route requests and handle caching.

## 3. Push to the Cache

With your infrastructure initialized, it's time to populate the cache. For most workflows the clearest way is the `push` command: hand it a Nix installable — a `./result` symlink, a flake reference, or a store path — and Aeroflare builds it if needed, then prepares, compresses, and uploads it directly to your registry.

```bash
# Push a build result
nix run github:ItzEmoji/aeroflare -- push ./result

# ...or a flake reference (built first if it isn't already)
nix run github:ItzEmoji/aeroflare -- push nixpkgs#hello
```

You can also point it at an explicit store path with `--store-path`, or push many at once from a file with `--input` — see [Cache Population](../how-to/cache-population.md) for the details. `push` uploads straight to the registry, so the proxy does not need to be running for this step.

### Alternative: build and push in one step with `run`

If you'd rather build and upload together, the `run` wrapper executes your Nix command through the proxy and automatically pushes any output paths it produces.

> **Important:** Currently, if you want Aeroflare to successfully push the resulting artifacts, you must pass the `--print-out-paths` flag to your Nix build command so Aeroflare knows what to upload.

```bash
nix run github:ItzEmoji/aeroflare -- run -- nix build .#default --print-out-paths
```

Its lifecycle:
1. **Pulling**: if the required build outputs already exist in your remote cache, they are pulled immediately, bypassing local compilation entirely.
2. **Execution**: if the artifacts are missing, the standard `nix build` command executes locally.
3. **Pushing**: upon successful build completion, Aeroflare isolates the new Nix store paths and uploads them as compressed blobs to your configured backend.

Congratulations! You've successfully configured and used Aeroflare to accelerate your Nix builds.
