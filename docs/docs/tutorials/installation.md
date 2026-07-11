---
sidebar_position: 3
title: Installation
---

# Installation

Aeroflare distributes binaries via Nix flakes, prebuilt release archives, and standard Go toolchains.

Two binaries are shipped. `aeroflare` is the interactive CLI. [`aeroflare-ci`](../explanation/aeroflare-ci.md)
is the non-interactive CI runner, which the GitHub Action wraps.

## Option 1: Nix Flakes (Recommended)

If you have a modern Nix installation with flakes enabled, you can run Aeroflare directly without installing it globally.

```bash
nix run github:ItzEmoji/aeroflare -- --help
```

To install it into your user profile permanently:

```bash
nix profile install github:ItzEmoji/aeroflare
```

Both binaries are in the package's `bin/`, so `aeroflare-ci` comes along with it. To run it without installing:

```bash
nix shell github:ItzEmoji/aeroflare --command aeroflare-ci --config .aeroflare-ci.yaml
```

## Option 2: Prebuilt Release Binaries

Each [GitHub release](https://github.com/ItzEmoji/aeroflare/releases) attaches a
compressed archive per binary and architecture:

| Asset | Contents |
|---|---|
| [`aeroflare-x86_64.tar.zst`](https://github.com/ItzEmoji/aeroflare/releases/latest/download/aeroflare-x86_64.tar.zst) | `aeroflare`, Linux x86_64 |
| [`aeroflare-aarch64.tar.zst`](https://github.com/ItzEmoji/aeroflare/releases/latest/download/aeroflare-aarch64.tar.zst) | `aeroflare`, Linux aarch64 |
| `aeroflare-ci-x86_64.tar.zst` | `aeroflare-ci`, Linux x86_64 |
| `aeroflare-ci-aarch64.tar.zst` | `aeroflare-ci`, Linux aarch64 |

```bash
version=1.8.0
arch=x86_64   # or aarch64

curl -fsSL -O "https://github.com/ItzEmoji/aeroflare/releases/download/v${version}/aeroflare-${arch}.tar.zst"
tar --zstd -xf "aeroflare-${arch}.tar.zst"
sudo mv aeroflare /usr/local/bin/
```

The archives are built with [build provenance attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds).
Verify one before trusting it:

```bash
gh attestation verify "aeroflare-${arch}.tar.zst" --repo ItzEmoji/aeroflare
```

:::caution Availability
Release archives are attached from **v1.8.0** onward. Earlier tags, including
`v1.7.0`, carry source only, and these downloads will 404 against them. The
archives are also **Linux-only** — on macOS or other platforms, use Nix or build
from source.
:::

## Option 3: Building from Source (Go)

Aeroflare requires Go 1.21 or later. 

1. Clone the repository:
   ```bash
   git clone https://github.com/ItzEmoji/aeroflare.git
   cd aeroflare
   ```

2. Compile the binaries:
   ```bash
   go build -o aeroflare .
   go build -o aeroflare-ci ./cmd/aeroflare-ci
   ```

3. Move them to your `PATH`:
   ```bash
   sudo mv aeroflare aeroflare-ci /usr/local/bin/
   ```


## Verification

Verify the installation by checking the version output.

```bash
aeroflare version
```
