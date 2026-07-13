---
sidebar_position: 3
title: Repository Layout & Codebase
---

# Repository Layout & Codebase Walkthrough

For contributors and advanced users, understanding exactly how Aeroflare is structured makes it much easier to debug issues or extend functionality. 

Aeroflare is written in Go and structured into two primary domains: the **CLI boundary** (`cmd/`) and the **Core library** (`internal/`), following the [golang-standards/project-layout](https://github.com/golang-standards/project-layout) convention.

## The `cmd/` Directory (CLI Boundary)

This directory contains the [Cobra](https://github.com/spf13/cobra) command definitions. Each file here maps directly to a subcommand you can run in your terminal. 

These files are deliberately kept thin. They are responsible for parsing flags, reading configuration via [Viper](https://github.com/spf13/viper), and passing arguments into the `internal/` core logic.

* `root.go`: The entrypoint of the CLI. Initializes Viper and loads `aeroflare.yaml`.
* `proxy.go`: Maps to `aeroflare proxy`. Invokes the `internal/proxy` package.
* `run.go`: Maps to `aeroflare run`. Starts the ephemeral proxy and wraps the Nix command.
* `init.go` / `configure.go` / `settings.go`: Maps to the interactive wizards (`aeroflare init` and `aeroflare settings`). These invoke the `huh` TUI forms.
* `auth*.go`: Handles all `aeroflare auth` subcommands, routing credential resolution to the `internal/secrets` package.
* `push.go`: CLI binding for the cache push command.

## The `internal/` Directory (Core Logic)

This is where the actual magic happens. The logic is heavily decoupled so that network operations, local file preparations, and proxying do not depend on CLI state. The storage-facing packages form a strict dependency layering — `backend` → `oci` — so lower layers never import the ones above them.

### 1. Networking & Storage
* `internal/oci/`: **The most critical package in the project.** It implements the OCI network layer (`network.go`), using `google/go-containerregistry` to stream `.nar` blobs as OCI layers and map Nix `.narinfo` metadata onto OCI Manifest Annotations (`vnd.aeroflare.nar.*`). It also owns registry credentials (`auth.go` — which delegates the token exchange, the retry policy and the re-authentication on expiry to go-containerregistry rather than reimplementing them), annotation parsing (`oci.go`), and writing the `cache-config` manifest (`config_manifest.go`).
* `internal/backend/`: The `CacheBackend` abstraction and its `NativeBackend` implementation (`native.go`), which publishes each completed push as its own OCI image. Also defines the `PushReceipt` type threaded through the push pipeline.

### 2. The Proxy Server
* `internal/proxy/`: Contains the HTTP server logic that tricks the Nix daemon into thinking it's talking to a standard binary cache.
  * `proxy_server.go`: Intercepts `/*.narinfo` and `/*.nar` HTTP requests. It parses the incoming Nix Hash, and dynamically translates the request into an OCI manifest fetch using `network.go`. It reconstructs the `.narinfo` plain text format on the fly.
  * `token_manager.go`: Handles authorization headers if the upstream registry requires authentication.

### 3. Execution Wrapper
* `internal/run/`: Implements the `aeroflare run` logic.
  * It spawns the proxy server on an ephemeral port.
  * It instruments the `nix` subprocess with `--option extra-substituters`.
  * Post-execution, it computes the newly generated Nix derivations and triggers a concurrent push operation.

### 4. Nix Artifact Preparation
* `internal/prepare/`: Before a local Nix store path can be pushed, it must be packaged.
  * This package generates the `.nar` (Nix Archive) format from local store paths.
  * It compresses the archive (e.g., using `zstd`).
  * It extracts dependencies and computes cryptographic hashes to generate the `.narinfo` map.

### 5. Security & Authentication
* `internal/secrets/`: Abstracts token storage. It integrates directly with the OS Native Keychain (using `zalando/go-keyring`) to ensure Cloudflare and GitHub tokens are encrypted at rest.
* `internal/auth/`: Implements OAuth flows and token validation with remote providers (e.g., verifying a GitHub PAT has `write:packages` scopes).

### 6. Wizards & UI
* `internal/init/`: Contains the complex infrastructure provisioning logic. It talks directly to the Cloudflare API to provision Edge Workers based on user selections.
* `internal/ui/`: Shared terminal UI components (progress bars, spinners, and customized `huh` themes) used across the CLI.

---

## How Data Flows Through the Codebase

To truly understand Aeroflare, let's trace exactly what happens when you run `aeroflare push --store-path /nix/store/abc-package`:

1. **Entry**: `cmd/push.go` parses the flags and passes `/nix/store/abc-package` to the core.
2. **Prepare**: `internal/prepare` serializes the directory into a `.nar` file and compresses it via `zstd`. It generates a `.narinfo` struct in memory.
3. **Upload Blob**: `internal/oci/network.go` opens a stream to the OCI Registry (e.g., GHCR). It uploads the compressed `.nar` as a raw `v1.Layer`.
4. **Map Metadata**: `internal/oci/network.go` creates a new OCI Manifest. It takes the `.narinfo` struct and maps every field (FileHash, StorePath, Sig) into `vnd.aeroflare.nar.*` annotations on the manifest.
5. **Tagging**: The manifest is tagged with `abc` (the 32-character Nix hash) and pushed to the registry. The operation completes with O(1) fetch capability guaranteed for the future.
