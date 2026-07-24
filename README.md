<p align="center">
  <img src="docs/static/img/favicon.svg" alt="Aeroflare logo" width="180" />
</p>

<h1 align="center">Aeroflare</h1>

> High-performance OCI-backed Nix binary cache proxy written in Go.

Aeroflare bridges the Nix ecosystem and standard container registries (such as GitHub Container Registry or Docker Hub) to act as a stateless, zero-infrastructure binary substituter.

---

## Key Features

- **Stateless Proxying**: Retains zero local binary state. Streams `.nar` blobs directly from OCI.
- **O(1) Manifest Lookups**: Tags artifacts directly with the 32-character Nix store path hash, enabling instantaneous lookups.
- **Interactive Provisioning**: A built-in setup wizard for GitHub, GitLab, and Cloudflare Worker deployment.
- **Flexible Publishing**: Push a flake output, a `./result`, or a store path straight to the cache with `aeroflare push`, or wrap a build end-to-end with the `aeroflare run` execution wrapper.
- **Native OCI Storage**: Each package is one OCI image tagged with its store hash — NAR blobs as layers, `narinfo` as manifest annotations. No separate metadata store.

---

## Quick Start

### 1. Initialize
Run the interactive onboarding wizard to configure credentials and provision resources:
```bash
nix run github:ItzEmoji/aeroflare -- init
```

### 2. Build & Push
Hand `push` a Nix installable — a `./result` symlink, a flake reference, or a store path — and Aeroflare builds it if needed, then uploads it straight to the cache:
```bash
nix run github:ItzEmoji/aeroflare -- push ./result
nix run github:ItzEmoji/aeroflare -- push nixpkgs#hello
```
You can also target an exact store path with `--store-path`, or push many at once with `--input`. `push` uploads directly to the registry, so no proxy needs to be running.

<details>
<summary>Alternative: build and push in one step with <code>run</code></summary>

The `run` wrapper executes your Nix command through the proxy and pushes any output paths automatically:

```bash
nix run github:ItzEmoji/aeroflare -- run -- nix build .#default --print-out-paths
```

*Note: The `--print-out-paths` flag is necessary for the `run` command to know which store paths were built and need to be cached.*
</details>

---

## Docker

Run the proxy as a container — no Nix or Go toolchain required on the host:

```bash
docker run -e AEROFLARE_CACHE=<org>/<cache> -p 8080:8080 ghcr.io/itzemoji/aeroflare-proxy
```

Full guide, including private-cache credentials and a persistent
`docker-compose.yml` setup:
[Docker](https://aeroflare.pages.dev/docs/how-to/docker).

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

Or hand it `builds: all` to discover and build every `packages.<system>.*`,
`devShells.<system>.*` and `nixosConfigurations.<host>` the flake exposes — no
list to keep up to date as the repository grows:

```yaml
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          builds: all
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

Everything under `pkg/` is public and importable; `internal/` is the CLI's own plumbing and cannot be imported from outside the module.

```
.
├── cmd/                # Binary entry points (aeroflare, aeroflare-ci)
├── pkg/                # Public API — see "Using Aeroflare as a Go library"
│   ├── oci/            # OCI registry transport: layers, pushers, token exchange, retry
│   ├── prepare/        # NAR serialization, hashing, compression, signing, upstream lookup
│   ├── push/           # Push pipeline (prepare -> upload), Reporter-driven
│   ├── proxy/          # Substituter HTTP server, handlers, token management
│   ├── cmd/            # Cobra command tree, one package per command
│   ├── cmdutil/        # Factory + the CLI's config/credential resolution (Viper, env, keyring)
│   └── iostreams/      # stdin/stdout/stderr abstraction
├── internal/           # CLI-only packages, not importable by other modules
│   ├── auth/           # OAuth token flows, device authorization
│   ├── secrets/        # Credentials manager (keyring + plaintext fallback)
│   ├── backend/        # CacheBackend abstraction + native OCI-tag implementation
│   ├── ci/             # `aeroflare-ci` build orchestration
│   ├── run/            # `aeroflare run` build wrapper
│   ├── init/           # Interactive provisioning wizards
│   └── ui/             # Shared terminal UI components
└── docs/               # Docusaurus documentation website
```

---

## Using Aeroflare as a Go library

Aeroflare's engines are importable, not just runnable. The packages under `pkg/` are documented for `go doc`:

| Package | What it does |
| --- | --- |
| [`pkg/oci`](pkg/oci) | Registry client: token exchange, blobs, manifests |
| [`pkg/prepare`](pkg/prepare) | Nix store path → NAR + narinfo (hashing, compression, signing) |
| [`pkg/push`](pkg/push) | The NAR/narinfo → registry upload pipeline |
| [`pkg/proxy`](pkg/proxy) | The Nix substituter HTTP server |

```console
$ go doc github.com/itzemoji/aeroflare/pkg/push
$ go doc github.com/itzemoji/aeroflare/pkg/oci ExchangeToken
```

These packages take their configuration as explicit parameters. They do not read Viper, the environment, or the OS keyring, and they never write to stdout — resolving credentials and rendering progress are the caller's job. `pkg/cmdutil` holds the CLI's own answers to those questions and is a worked example. Each package's `doc.go` carries a runnable example.

### API stability

> **The Go API under `pkg/` is _not_ covered by this module's semantic version.**

Aeroflare is versioned as a command-line tool. A release may change, rename, or remove any exported Go symbol without a major version bump, and the project makes no compatibility promise to importers. If these packages are useful to you, import them — but pin an exact version and expect to make adjustments when you upgrade.

---

## Documentation

For full guides, reference manuals, and architecture explanations, check out the [documentation site](https://aeroflare.pages.dev) or browse [docs/docs/](docs/docs/).

