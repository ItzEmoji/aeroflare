---
id: cache
title: Cache Operations
sidebar_position: 3
---

# Cache Operations

The Aeroflare CLI provides three core cache-related commands that interface with OCI registries to store, retrieve, and transparently proxy Nix build artifacts. The underlying mechanics of these commands bridge standard POSIX/Nix conventions with our OCI-backed registry storage.

## `push`

The `push` command (`pkg/cmd/push/push.go`) handles the serialization and uploading of Nix store paths into the configured OCI registry.

### Mechanics & State Changes

1. **Registry Resolution**: Uses `network.GetRegistryAndRepository()` to determine the target OCI registry and fetches the associated authentication token.
2. **Configuration Parsing**: Passes the CLI arguments, `--store-path`, `--input`, and `os.Stdin` to `push.ParseConfig()` to resolve the target paths.
3. **Preflight Evaluation**: Executes `push.Preflight(cfg)` to calculate a deployment plan. This involves:
   - Validating paths and checking upstream cache (`--upstream-cache`) to determine which paths already exist.
   - Determining references to prepare if `--prepare-refs` is enabled.
4. **Execution**: If the preflight plan succeeds, it outputs a summary via `push.DisplaySummary(plan)` and performs the upload via `push.RunPush(plan)`.

### Flags

- `--store-path`: Explicit Nix store path to prepare and push (e.g., `/nix/store/xxx-yyy`).
- `--input`: File containing store paths (one per line, `#` for comments).
- `--compression` (default: `zstd`): Compression algorithm to use (`zstd`, `xz`, `gzip`, `none`).
- `--upstream-cache` (default: `https://cache.nixos.org`): Upstream binary cache URL. Leaving this empty skips reference checking.
- `--workers` (default: `50`): Number of concurrent workers for uploading blobs.
- `--prepare-refs` (default: `true`): Prepares references that are not present on the upstream cache.
- `--signing-key`: Path to the Nix signing private key file to sign the generated `.narinfo`.
- `--keep` (default: `false`): Keeps generated `.nar` and `.narinfo` files locally after the push is completed.
- `--force` (default: `false`): Forces a push even if the files exist in the index or upstream cache.

## `run`

The `run` command (`pkg/cmd/run/run.go`) wraps arbitrary shell commands to transparently push Nix store paths generated or referenced during the execution.

### Mechanics & State Changes

1. **Index Directory Resolution**: Determines the proxy index directory via `getIndexDir(repository)`.
2. **Command Execution**: Invokes `run.ExecuteCommand()` passing the wrapped command, registry, repository, index directory, and token. This executes the target command, potentially injecting the proxy substituting mechanism.
3. **Path Detection**: `ExecuteCommand` captures standard output and identifies generated Nix store paths.
4. **Transparent Push**: If target paths are detected, `run.go` automatically triggers the `push` machinery. It constructs a `push.PushConfig` reusing the same CLI flags as the `push` command, runs `push.Preflight()`, and subsequently `push.RunPush()`.

### Flags

The `run` command re-uses the flags from `push` to define cache upload behavior:
- `--compression`, `--upstream-cache`, `--workers`, `--prepare-refs`, `--signing-key`, `--keep`, `--force`.

## `proxy`

The `proxy` command (`pkg/cmd/proxy/proxy.go`) initializes a local HTTP proxy server designed to intercept and serve Nix binary cache requests, querying the OCI registry or falling back to upstream caches.

### Mechanics & State Changes

1. **Environment Configuration**: The proxy heavily relies on environment variables for configuration instead of CLI flags.
2. **Index Directory Setup**: Uses `getIndexDir()` to define where the proxy stores its local index. It checks environment variables in the following order of precedence:
   - `AEROFLARE_INDEX_DIR`
   - `NIXCACHE_INDEX_DIR`
   - `CACHE_DIRECTORY`
   - Fallback: `~/.cache/aeroflare-proxy/<repository-slug>` (slashes in the repository are replaced with `--`).
3. **Server Initialization**: Intercepts `SIGINT` and `SIGTERM` to gracefully cancel the context.
4. **Start**: Calls `proxy.StartProxy()` which binds to the designated address/port and begins serving requests, using the resolved registry token for authentication.

### Environment Variables

- `NIXCACHE_PORT` (default: `37515`): The TCP port the proxy binds to.
- `NIXCACHE_LISTEN` (default: `127.0.0.1`): The interface address to listen on.
- `NIXCACHE_UPSTREAM` (default: `https://cache.nixos.org`): Space-separated list of upstream cache URLs.
