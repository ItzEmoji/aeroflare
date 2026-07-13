---
id: subsystems
title: Core Subsystems
sidebar_position: 2
---

# Core Subsystems

This document provides a deep technical breakdown of the core subsystems within Aeroflare: Authentication & Secrets, Storage Integration, and the Push Pipeline. It is intended for developers and contributors to understand the mechanics of these systems.

## Authentication & Secrets Management

Aeroflare manages authentication primarily around accessing OCI registries (like GitHub Container Registry or Docker Hub) and securely storing user credentials.

### Token Resolution (`internal/auth/resolver.go`)
The `Resolver` struct is responsible for locating authentication tokens. It follows a strict hierarchy of sources:
1. **Command-line flags** (highest priority).
2. **Environment variables** (e.g., `GITHUB_TOKEN`, `GH_TOKEN`, `oci_token`).
3. **Secrets Manager** (secure local keychain or fallback).

Specific functions like `ResolveGithubToken` and `ResolveRegistryToken` wrap the resolver with the correct environment variables and secret keys depending on the target registry (e.g., mapping `ghcr.io` to the GitHub token).

### Registry Authentication (`pkg/oci/auth.go`)
OCI registries require a token exchange—trading a Personal Access Token (PAT) via Basic Auth for a short-lived Bearer token. **Aeroflare does not implement that exchange.** It is a protocol (the Docker Registry v2 token flow), and `go-containerregistry` implements it, so Aeroflare's job is only to say *which credential it holds*.

A credential is an `authn.Authenticator`, built by one of three constructors:
- `PasswordAuth(username, password)` — a password or PAT. The registry exchanges it. Every credential the CLI can resolve takes this path; nothing inspects the token's prefix to guess what it is.
- `BearerAuth(token)` — a token the caller has already exchanged, sent verbatim.
- `nil` — anonymous, which is enough to read a public cache.

`Transport()` and `Client()` wrap `transport.NewWithContext`, which pings `/v2/`, parses the `WWW-Authenticate` challenge to find the realm and service (which may be on a different host — Docker Hub challenges to `auth.docker.io`), performs the exchange, and re-authenticates whenever the registry challenges again, whether because the token expired or because the request needs a wider scope. Nothing in Aeroflare tracks token lifetimes.

This is why supporting a new registry is a matter of configuration rather than code.

### Device Flow Auth (`internal/auth/github.go`)
For interactive logins, Aeroflare implements the OAuth 2.0 Device Authorization Grant flow for GitHub:
- `RequestDeviceCode` hits `/login/device/code` to obtain a `user_code` for the user to enter in their browser and a `device_code`.
- `PollAccessToken` polls `/login/oauth/access_token` until the user authorizes the app, handling `authorization_pending` and `slow_down` responses appropriately.

### Secrets Manager (`internal/secrets/manager.go`)
The `secrets.Manager` interface abstracts credential storage.
- It uses the `zalando/go-keyring` library to securely store credentials in the OS native keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service).
- **Fallback Mechanism:** If the keychain is unavailable (e.g., in CI environments or headless servers), it gracefully falls back to plain-text JSON storage in `~/.config/aeroflare/secrets.json`.
- An internal `_keys_index` is maintained to keep track of which keys are managed by Aeroflare.

## Storage Integration

Aeroflare stores everything in an OCI registry. Each Nix package is published as
its own OCI image, tagged with its store hash: the compressed `.nar` is the
image's layer, and the `.narinfo` metadata is carried in the manifest
annotations (`vnd.aeroflare.nar.*`). There is no separate metadata store to
provision or keep in sync.

## The Push Pipeline (`internal/push/push.go`)

The Push Pipeline is the engine that converts local Nix store paths into OCI-compliant artifacts and pushes them to remote registries.

### Configuration & Preflight
The pipeline parses paths from CLI arguments, an input file, or `stdin`. 
Before processing, a `Preflight` phase (and subsequent cache index checks) filters out paths that are already present in the remote registry to avoid redundant work.

### Batch Preparation
Paths are processed in chunks (default 100) to manage memory and disk I/O.
- The `prepare.PrepareBatch` step runs the local Nix daemon to generate NAR files and sign `.narinfo` files into a temporary directory (`/tmp/aeroflare-push-*`).
- If `KeepFiles` is not specified, this temporary directory is aggressively cleaned up to prevent disk exhaustion.

### Concurrent Upload Strategy
The upload phase uses `golang.org/x/sync/errgroup` to execute uploads concurrently, bounded by the configured number of workers.
To achieve maximum performance, Aeroflare employs a "brutal speed" optimization:
- `network.NewLayerFast` creates the OCI layer representation without reading and hashing the entire NAR file on disk beforehand. It relies on the pre-computed size and hash from the `.narinfo` file.

### Native OCI Push
Each receipt is published as its own OCI image via `network.PushNarPackage`,
using standard OCI manifest layouts: the compressed `.nar` becomes the image
layer, and the `.narinfo` fields are written as `vnd.aeroflare.nar.*` manifest
annotations. The image is tagged with the package's store hash, giving O(1)
lookups on subsequent fetches.
