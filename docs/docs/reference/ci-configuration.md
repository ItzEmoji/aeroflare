---
sidebar_position: 3
title: CI Configuration Schema
---

# CI Configuration Schema

The `.aeroflare-ci.yaml` file configures [`aeroflare-ci`](../explanation/aeroflare-ci.md)
and the [GitHub Action](../how-to/github-action.md) in `config` mode. It is
validated by a JSON Schema published alongside the source:

```
https://raw.githubusercontent.com/ItzEmoji/aeroflare/v1/schema/aeroflare-ci.schema.json
```

Reference it from the first line of your config to get completion and inline
validation in any editor running a YAML language server:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/ItzEmoji/aeroflare/v1/schema/aeroflare-ci.schema.json
```

The schema sets `additionalProperties: false`, so an unknown or misspelled key
is reported rather than silently ignored.

## Keys

| Key | Type | Required | Default | Description |
|---|---|---|---|---|
| `builds` | array of strings | **yes** | — | Nix flake installables to build. At least one. The entry `all` discovers them. |
| `caches` | array of strings | **yes** | — | Push targets, each `<registry>;<repository>`. At least one. |
| `compression` | `zstd` \| `xz` \| `gzip` \| `none` | no | `zstd` | NAR compression algorithm. |
| `signing-key` | string | no | unsigned | Path to a signing key, **or** the name of an environment variable holding the key material. |
| `workers` | integer ≥ 1 | no | `50` | Concurrent upload workers. |
| `upstream-cache` | string or array of strings | no | `https://cache.nixos.org` | Caches whose paths are skipped. `none` disables filtering. |
| `release-repo` | string `owner/repo` | no | `ItzEmoji/aeroflare` | Repository the GitHub Action downloads `aeroflare-ci` from. |
| `release-version` | string | no | the pinned action's version | Release to download, or `latest`. |
| `skip-attestation` | boolean | no | `false` | Skip provenance verification of the downloaded asset. |

Every entry in `builds` is pushed to every entry in `caches`. There is no way to
route one installable to one cache and a different installable elsewhere.

The last three keys configure **the GitHub Action's install step**, not a build.
`aeroflare-ci` itself ignores them — see [Release source keys](#release-source-keys).

## Complete example

```yaml title=".aeroflare-ci.yaml"
# yaml-language-server: $schema=https://raw.githubusercontent.com/ItzEmoji/aeroflare/v1/schema/aeroflare-ci.schema.json
builds:
  - .#default
  - .#packages.x86_64-linux.foo

caches:
  - ghcr.io;itzemoji/nix-cache      # primary
  - docker.io;itzemoji/nix-cache

compression: zstd
workers: 100
signing-key: NIX_SIGNING_KEY

upstream-cache:
  - https://cache.nixos.org
  - https://nix-community.cachix.org
```

## Key semantics

### `builds`

Each entry is a Nix flake installable, built with `nix build <installable>`.

The single entry `all` is a **sentinel**: instead of naming one installable, it
expands at run time into everything the flake in the working directory exposes
for the runner's system.

```yaml title=".aeroflare-ci.yaml"
builds:
  - all
caches:
  - ghcr.io;itzemoji/nix-cache
```

Three output classes are discovered:

| Class | Built as |
|---|---|
| `packages.<system>.<name>` | `.#packages.<system>.<name>` |
| `devShells.<system>.<name>` | `.#devShells.<system>.<name>` |
| `nixosConfigurations.<host>` | `.#nixosConfigurations.<host>.config.system.build.toplevel` |

A class the flake does not expose contributes nothing; it is not an error. NixOS
configurations built for another platform are skipped, since the runner cannot
build them. Discovery only ever looks at the current checkout — there is no
syntax for discovering a remote flake.

`all` may be mixed with explicit installables, and duplicates are dropped:

```yaml
builds:
  - all
  - github:some/other#tool
```

:::note
Discovery reads the flake's outputs directly and applies no `meta` filtering.
Unlike the NUR template's `ci.nix`, a package marked `meta.broken` or unfree is
still attempted, and fails the run when it fails to build. Only the derivation's
default output is built, not `dev`/`man`, and attribute sets marked
`recurseForDerivations` are not descended into.
:::

### `caches`

Each entry is `<registry>;<repository>`, separated by a semicolon rather than a
slash so the repository may itself contain slashes.

The **first entry is the primary cache**. It backs the substituter used during
the build, and its push token is validated before any build starts — a missing
one aborts the run immediately. A missing token for any other cache fails only
that push.

An `https://` or `http://` prefix on the registry is stripped, so
`https://ghcr.io;me/cache` and `ghcr.io;me/cache` are equivalent.

:::note
The schema's pattern permits exactly one semicolon. The binary is more lenient —
it splits on the first one — but stay within the schema so editor validation
keeps working.
:::

Push tokens never appear in this file. They come from `AEROFLARE_TOKEN_<HOST>`
environment variables; see [Token resolution](../explanation/aeroflare-ci.md#token-resolution).

### `signing-key`

The value is resolved in this order:

1. If it names an environment variable that is set and non-empty, that
   variable's contents are the key material.
2. Otherwise it is a filesystem path.
3. If neither, the run fails.

`signing-key: NIX_SIGNING_KEY` reads `$NIX_SIGNING_KEY`; `signing-key: ./key.sec`
reads the file. Prefer the environment form in CI. Omit the key entirely to push
unsigned NARs.

### `upstream-cache`

Accepts a bare string or a list:

```yaml
upstream-cache: https://cache.nixos.org
```

```yaml
upstream-cache:
  - https://cache.nixos.org
  - https://nix-community.cachix.org
```

An explicit value **replaces** the default rather than extending it. If you name
another upstream and still want nixpkgs paths skipped, list `cache.nixos.org`
yourself.

`upstream-cache: none` disables filtering entirely and uploads the full closure,
making the cache self-contained. It cannot be combined with other entries.

:::warning
The schema rejects `upstream-cache: []`. The binary alone would read an empty
list as "unset" and substitute the default — the opposite of what an empty list
suggests. Write `none` if you mean no filtering.
:::

### Release source keys

`release-repo`, `release-version` and `skip-attestation` decide **which
`aeroflare-ci` binary the GitHub Action downloads**. They exist for running a
fork or a test build instead of the published upstream release.

```yaml title=".aeroflare-ci.yaml"
release-repo: me/aeroflare-fork
release-version: latest
skip-attestation: true   # this fork publishes no attestations
```

`release-version` accepts `1.11.0`, `v1.11.0`, or `latest`. Omitted, it resolves
to the version declared by the action ref you pinned. `latest` is what a fork or
throwaway test repository usually wants, since its release numbering rarely
tracks upstream's.

:::warning `skip-attestation` disables a supply-chain check
Every upstream release archive carries SLSA build provenance, and the Action
verifies it on every run. Setting `skip-attestation: true` means the binary you
execute in CI is unverified. Set it only for a repository you control that
publishes no attestations — and prefer publishing them, since the release
workflow a fork inherits already does.
:::

Three consequences of these keys being read before the binary exists:

- **`aeroflare-ci` ignores them.** They only take effect through the Action.
  Running the binary directly, or from [another CI system](../how-to/ci-integration.md),
  they do nothing.
- **They are read by a minimal parser.** The Action's install step extracts them
  from the file with `sed`, because the binary that normally parses the config is
  the very thing being downloaded, and no YAML tool is guaranteed on a runner. It
  handles top-level keys with optionally-quoted scalar values and inline
  comments. Anchors, block scalars and multi-document files are not supported for
  *these three keys only*; every other key is parsed normally by the binary.
- **Precedence is input, then file, then default.** The `release-repo`,
  `release-version` and `skip-attestation` action inputs override the file. The
  legacy `AEROFLARE_REPO` environment variable still works, but ranks below the
  file.

## Precedence

Flags and environment variables override this file, and **list values replace
rather than merge**. A single `--build` alongside a file listing three
installables builds exactly one.

This is why the GitHub Action refuses `config` together with `builds` or
`cache`. See [Configuration resolution](../explanation/aeroflare-ci.md#configuration-resolution)
for the full table.

## Related

- [GitHub Action](../how-to/github-action.md) — using this file in `config:` mode
- [CI Integration](../how-to/ci-integration.md) — GitLab CI and generic runners
- [The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md) — resolution, tokens, exit codes
