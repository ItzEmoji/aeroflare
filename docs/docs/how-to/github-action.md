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

### Build everything the flake exposes

Replace the list with the single entry `all` to build and cache every
`packages.<system>.*`, `devShells.<system>.*` and `nixosConfigurations.<host>`
in the checked-out flake — useful for a package repository, where the list
would otherwise need updating with every new package.

```yaml
      - uses: ItzEmoji/aeroflare@v1
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          builds: all
```

Only outputs for the runner's system are discovered, so a build matrix across
runners caches each platform. Note that no `meta` filtering happens — a package
marked `broken` or unfree is attempted and fails the job. See
[`builds`](../reference/ci-configuration.md#builds) for the full semantics.

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

## Running a fork or a test build

By default the Action downloads the `aeroflare-ci` release asset from
`ItzEmoji/aeroflare`, at the version the ref you pinned declares. Three inputs
redirect that — for testing a change before it ships, or for running a fork you
maintain:

```yaml
      - uses: ItzEmoji/aeroflare@v1
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          builds: all
          release-repo: me/aeroflare-fork
          release-version: latest
          skip-attestation: true
```

| Input | Default | Notes |
|---|---|---|
| `release-repo` | `ItzEmoji/aeroflare` | `owner/repo`. A bare name, a URL, or an `@ref` suffix is rejected with a pointed message rather than an opaque `gh` failure. |
| `release-version` | the pinned action's version | `1.11.0`, `v1.11.0`, or `latest`. |
| `skip-attestation` | `false` | `true` skips provenance verification. |

Reach for `release-version: latest` on a fork or test repository — its release
numbering rarely matches upstream's, and the default resolves to a tag that
probably does not exist there.

The same three settings can live in the config file instead, as `release-repo`,
`release-version` and `skip-attestation`. They are read by the Action's install
step with a deliberately minimal parser, since the binary that normally reads the
config is the one being downloaded; see
[Release source keys](../reference/ci-configuration.md#release-source-keys) for
the caveats. An input always overrides the file.

:::warning `skip-attestation` disables a supply-chain check
It exists for repositories that publish no attestations. Set it and the binary
you execute in CI is unverified. A fork inherits the release workflow that
publishes provenance, so publishing attestations is usually the better fix.
:::

:::note Private forks
Downloads use the `token` input, which defaults to `github.token` and cannot
read a private repository in another org. Pointing `release-repo` at one means
passing a PAT as `token` — the same token the Action uses for `ghcr.io` pushes.
:::

The undocumented `AEROFLARE_REPO` environment variable still works and still
means the same thing, but it ranks below both the input and the config file.

## Verifying the binaries yourself

Every release archive carries SLSA build provenance, which the Action checks
on every run — against `release-repo`, so a fork is verified against itself.
To check by hand:

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
no archives. Pin the Action to a release that does — or, if you set
`release-repo`, point `release-version` at a tag that exists there.
`release-version: latest` sidesteps the mismatch.

**`'release-repo' must be owner/repo, got '…'`**
The input takes a repository, not a URL and not a ref: `me/aeroflare-fork`,
never `https://github.com/me/aeroflare-fork` or `me/aeroflare-fork@v1`. Use
`release-version` for the ref.

**`provenance verification failed for … ; if this repo publishes no attestations…`**
A `release-repo` whose releases carry no SLSA provenance. Publishing
attestations from the fork is the better fix; `skip-attestation: true` is the
escape hatch, at the cost of the check.

For errors that aren't specific to the Action — token resolution, partial
push failures, list-replace semantics — see
[CI Integration troubleshooting](./ci-integration.md#troubleshooting).

## Related

- [CI Configuration Schema](../reference/ci-configuration.md) — every
  `.aeroflare-ci.yaml` key, type, and default
- [The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md) — precedence,
  token resolution, exit codes
- [CI Integration](./ci-integration.md) — GitLab CI and generic runners
