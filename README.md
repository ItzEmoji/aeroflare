# Aeroflare

> High-performance OCI-backed Nix binary cache proxy written in Go.

Aeroflare bridges the Nix ecosystem and standard container registries (such as GitHub Container Registry or Docker Hub) to act as a stateless, zero-infrastructure binary substituter.

---

## Key Features

- **Stateless Proxying**: Retains zero local binary state. Streams `.nar` blobs directly from OCI.
- **O(1) Manifest Lookups**: Tags artifacts directly with the 32-character Nix store path hash, enabling instantaneous lookups.
- **Interactive Provisioning**: A built-in setup wizard for GitHub, GitLab, and Cloudflare Worker deployment.
- **Execution Wrapper**: Run builds transparently with the `run` wrapper (`aeroflare run -- nix build`).
- **Native OCI Storage**: Each package is one OCI image tagged with its store hash — NAR blobs as layers, `narinfo` as manifest annotations. No separate metadata store.

---

## Quick Start

### 1. Initialize
Run the interactive onboarding wizard to configure credentials and provision resources:
```bash
nix run github:ItzEmoji/aeroflare -- init
```
### 3. Build & Cache
Execute a build and automatically push the outputs:
```bash
nix run github:ItzEmoji/aeroflare -- run -- nix build .#default --print-out-paths
```
*Note: The `--print-out-paths` flag is necessary for the `run` command to know which store paths were built and need to be cached.*


---

## GitHub Action

Build your flake outputs and push them to an OCI cache from CI. Nix must already
be on the runner — the action does not install it.

### Configless

One cache, builds listed inline:

```yaml
jobs:
  cache:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v5
      - uses: DeterminateSystems/nix-installer-action@v20
      - uses: ItzEmoji/aeroflare@v1
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          builds: |
            .#default
            .#packages.x86_64-linux.foo
```

`ghcr.io` authenticates with the workflow's `github.token` automatically. For any
other registry, pass `cache-token`.

### Upstream caches

By default only store paths missing from `https://cache.nixos.org` are uploaded,
so your cache holds your artifacts rather than a copy of nixpkgs. Name other
upstreams to skip their paths too:

```yaml
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          upstream-cache: |
            https://cache.nixos.org
            https://nix-community.cachix.org
```

An explicit `upstream-cache` **replaces** the default — list `cache.nixos.org`
yourself if you still want its paths skipped.

To upload the full closure, making the cache self-contained:

```yaml
        with:
          upstream-cache: none
```

### With a config file

Several caches, or settings you would rather keep in the repo:

```yaml
      - uses: ItzEmoji/aeroflare@v1
        with:
          config: .aeroflare-ci.yaml
        env:
          AEROFLARE_TOKEN_DOCKER_IO: ${{ secrets.DOCKERHUB_TOKEN }}
```

```yaml
# .aeroflare-ci.yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/ItzEmoji/aeroflare/v1/schema/aeroflare-ci.schema.json
builds:
  - .#default
caches:
  - ghcr.io;itzemoji/nix-cache
  - docker.io;myorg/nix-cache
compression: zstd
upstream-cache: https://cache.nixos.org
```

Every build is pushed to every cache. `config` cannot be combined with `builds`
or `cache` — an inline list replaces the file's list, so the action rejects the
ambiguity rather than silently discarding your config.

In config mode each registry's push token comes from a job-level environment
variable named `AEROFLARE_TOKEN_<HOST>`, where `<HOST>` is the registry
uppercased with `.` and `:` replaced by `_`.

### Requirements

- Linux runners only (`x86_64` or `aarch64`).
- Pin to `v1.8.0` or later. Earlier releases ship no binaries, and the action
  will tell you so.

### Verifying the binaries yourself

Every release archive carries SLSA build provenance, which the action checks on
every run. To check by hand:

```bash
gh attestation verify aeroflare-ci-x86_64.tar.zst --repo ItzEmoji/aeroflare
```

---

## Codebase Directory Map

```
.
├── cmd/                # Cobra CLI commands (flag parsing, Viper configurations)
│   ├── root.go         # Entry point, environment bindings
│   ├── proxy.go        # Proxy CLI command definition
│   ├── run.go          # CLI command for build wrapper
│   └── ...             # Settings, auth, and push CLI commands
├── internal/           # Core logic packages (decoupled from Cobra/CLI)
│   ├── oci/            # OCI registry transport: layers, pushers, auth, retry
│   ├── backend/        # CacheBackend abstraction + native OCI-tag implementation
│   ├── auth/           # OAuth token flows, Device authorization
│   ├── secrets/        # Credentials manager (keyring + plaintext fallback)
│   ├── proxy/          # HTTP server, proxy handlers, token management
│   ├── prepare/        # Local NAR serialization, compression, and signing
│   ├── push/           # Push pipeline (prepare -> backend publish)
│   ├── run/            # `aeroflare run` build wrapper
│   ├── init/           # Interactive provisioning wizards
│   └── ui/             # Shared terminal UI components
└── docs/               # Docusaurus documentation website
```

---

## Documentation

For full guides, reference manuals, and architecture explanations, check out the [documentation site](https://aeroflare.pages.dev) or browse [docs/docs/](docs/docs/).

