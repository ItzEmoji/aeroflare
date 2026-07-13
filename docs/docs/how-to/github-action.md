---
sidebar_position: 5
title: GitHub Action
---

# GitHub Action

`ItzEmoji/aeroflare` is a composite GitHub Action that wraps the
[`aeroflare-ci`](../explanation/aeroflare-ci.md) binary: it downloads the
release binary for the runner's architecture, translates the action's inputs
into flags and environment variables, and runs it. It knows nothing that the
binary doesn't — anything documented here is also reachable from the command
line, or from [another CI system](./ci-integration.md).

## Prerequisites

- **Nix must already be installed.** The Action does not install it — pair it
  with something like `DeterminateSystems/nix-installer-action`.
- **Flakes must be enabled**, because builds are flake installables.
- **The build user should be trusted.** Aeroflare injects its local
  substituter through `NIX_CONFIG`, and the Nix daemon ignores
  `extra-substituters` for untrusted users. An untrusted build still
  succeeds — it just rebuilds everything instead of substituting, which
  defeats the point.
- **A Linux runner, `x86_64` or `aarch64`.** These are the only
  architectures the release archives ship for.
- **Pin to `v1.8.0` or later.** Earlier releases ship no binaries, and the
  Action will tell you so rather than failing obscurely.

## Configless mode

One cache, builds listed inline. Suitable until you need a second registry.

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

`cache` accepts **exactly one** target. `cache-token` supplies its push
token; for `ghcr.io` you can omit it, because the Action passes the
workflow's `github.token` through as `GITHUB_TOKEN` and `ghcr.io` falls back
to it. By default only store paths missing from `https://cache.nixos.org`
are uploaded, so your cache holds your artifacts rather than a copy of
nixpkgs.

:::warning The `builds: |` indentation trap
`builds: |` is a YAML literal block scalar. A sibling input indented one
level too deep becomes another *line of the builds string*, not an input:

```yaml
        with:
          builds: |
            .#default
            upstream-cache: none   # ← wrong: this is now a build target
```

Nix would be handed the installable `upstream-cache: none`. Aeroflare
detects this and fails early with a pointed message rather than letting
`nix build` produce something baffling:

```
builds contains "upstream-cache: none", which is the "upstream-cache" action
input, not a flake installable: check your indentation
```

Dedent the input to be a sibling of `builds`.
:::

## Config mode

Use a config file when you have several caches, or want the settings
reviewed in your repository rather than buried in a workflow.

```yaml
jobs:
  cache:
    runs-on: ubuntu-latest
    permissions:
      contents: read     # gh release download + provenance verification
      packages: write    # push to ghcr.io
    steps:
      - uses: actions/checkout@v5
      - uses: DeterminateSystems/nix-installer-action@v20
      - uses: ItzEmoji/aeroflare@v1
        with:
          config: .aeroflare-ci.yaml
        env:
          AEROFLARE_TOKEN_DOCKER_IO: ${{ secrets.DOCKERHUB_TOKEN }}
          NIX_SIGNING_KEY: ${{ secrets.NIX_SIGNING_KEY }}
```

```yaml title=".aeroflare-ci.yaml"
# yaml-language-server: $schema=https://raw.githubusercontent.com/ItzEmoji/aeroflare/v1/schema/aeroflare-ci.schema.json
builds:
  - .#default
  - .#packages.x86_64-linux.foo

caches:
  - ghcr.io;itzemoji/nix-cache      # primary: backs the build substituter
  - docker.io;itzemoji/nix-cache

compression: zstd
workers: 100
signing-key: NIX_SIGNING_KEY        # the NAME of an env var, not a path

upstream-cache:
  - https://cache.nixos.org
  - https://nix-community.cachix.org
```

Three things about this file repay attention.

**`config` is mutually exclusive with `builds` and `cache`.** The Action
refuses the combination outright:

```
'config' and 'builds'/'cache' are mutually exclusive: an inline list replaces
the config file's list. Use one mode or the other.
```

It refuses because an inline list *replaces* the file's list rather than
extending it. Accepting both would silently discard the builds you wrote
down.

**`cache-token` is ignored in config mode.** It names a token for the single
`cache` input, which does not exist here. Supply one environment variable
per registry instead, named `AEROFLARE_TOKEN_<HOST>` with `.` and `:`
replaced by `_`. Above, `docker.io` needs `AEROFLARE_TOKEN_DOCKER_IO`;
`ghcr.io` needs nothing, because of its `GITHUB_TOKEN` fallback.

:::note Registries that check the username
The token is presented to the registry as a password, over Basic auth, with
the username defaulting to `token`. `ghcr.io` ignores the username, but Docker
Hub and GitLab check it: pair their token with `AEROFLARE_USERNAME_<HOST>` —
`AEROFLARE_USERNAME_DOCKER_IO` set to your Docker Hub account name, for
instance. Omit it and the registry answers `401`.
:::

**Cache order matters.** The first entry is the primary: it backs the
substituter used during the build, and its token is validated before
anything is built.

### Editor support

The `yaml-language-server` comment on line one gives you completion and
validation in any editor with a YAML language server. The schema is strict —
`additionalProperties: false` — so a typo like `upstream_cache` is flagged
as an unknown key rather than silently ignored.

The schema also rejects `upstream-cache: []`, which the binary alone would
read as "unset" and quietly replace with the default. Write
`upstream-cache: none` if you mean no filtering.

Every key, its type, and its default is listed in the
[CI Configuration Schema](../reference/ci-configuration.md).

## Verifying the binaries yourself

Every release archive carries SLSA build provenance, which the Action checks
on every run. To check by hand:

```bash
gh attestation verify aeroflare-ci-x86_64.tar.zst --repo ItzEmoji/aeroflare
```

## Troubleshooting

**`builds contains "…", which is the "…" action input`**
A YAML indentation mistake under `builds: |`. Dedent the input — see the
warning above.

**`release <tag> of <repo> ships no aeroflare-ci-<arch>.tar.zst`**
The Action resolves the release tag from the `version.json` at the ref you
pinned, then downloads that release's archive. Tags before `v1.8.0` publish
no archives. Pin the Action to a release that does.

For errors that aren't specific to the Action — token resolution, partial
push failures, list-replace semantics — see
[CI Integration troubleshooting](./ci-integration.md#troubleshooting).

## Related

- [CI Configuration Schema](../reference/ci-configuration.md) — every
  `.aeroflare-ci.yaml` key, type, and default
- [The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md) — precedence,
  token resolution, exit codes
- [CI Integration](./ci-integration.md) — GitLab CI and generic runners
