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

`ghcr.io` authenticates with the workflow's `github.token` automatically; any
other registry takes a `cache-token`. By default only store paths missing from
`https://cache.nixos.org` are uploaded, so your cache holds your artifacts
rather than a copy of nixpkgs.

Full guide, including advanced `config:` mode, multiple caches, GitLab CI,
generic runners, and binary verification:
[GitHub Action](https://aeroflare.pages.dev/docs/how-to/github-action).

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

