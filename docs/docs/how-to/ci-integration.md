---
sidebar_position: 5
title: CI Integration
---

# CI Integration

`aeroflare-ci` is a plain binary driven by flags, environment variables, and an
optional YAML file. The GitHub Action is a convenience wrapper around it, not a
privileged path. Anything the Action does, another CI system can do too.

This guide covers the Action's advanced `config:` mode first, then the same
capabilities on GitLab CI and on runners with no integration at all.

For the resolution rules these recipes depend on — precedence, token variable
names, signing-key overloading — see
[The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md).

## Prerequisites

On every platform:

- **Nix must already be installed.** Neither the Action nor the binary installs it.
- **Flakes must be enabled**, because builds are flake installables.
- **The build user should be trusted.** Aeroflare injects its local substituter
  through `NIX_CONFIG`, and the Nix daemon ignores `extra-substituters` for
  untrusted users. An untrusted build still succeeds — it just rebuilds
  everything instead of substituting, which defeats the point.

The Action additionally requires a **Linux runner** on `x86_64` or `aarch64`,
because that is what the release archives ship.

## GitHub Actions: configless mode

One cache, builds inline. Suitable until you need a second registry.

```yaml
      - uses: ItzEmoji/aeroflare@v1
        with:
          cache: ghcr.io;${{ github.repository_owner }}/nix-cache
          builds: |
            .#default
```

`cache` accepts **exactly one** target. `cache-token` supplies its push token;
for `ghcr.io` you can omit it, because the Action passes the workflow's
`github.token` through as `GITHUB_TOKEN` and `ghcr.io` falls back to it.

:::warning The `builds: |` indentation trap
`builds: |` is a YAML literal block scalar. A sibling input indented one level
too deep becomes another *line of the builds string*, not an input:

```yaml
        with:
          builds: |
            .#default
            upstream-cache: none   # ← wrong: this is now a build target
```

Nix would be handed the installable `upstream-cache: none`. Aeroflare detects
this and fails early with a pointed message rather than letting `nix build`
produce something baffling:

```
builds contains "upstream-cache: none", which is the "upstream-cache" action
input, not a flake installable: check your indentation
```

Dedent the input to be a sibling of `builds`.
:::

## GitHub Actions: config mode

Use a config file when you have several caches, or want the settings reviewed in
your repository rather than buried in a workflow.

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

**`config` is mutually exclusive with `builds` and `cache`.** The Action refuses
the combination outright:

```
'config' and 'builds'/'cache' are mutually exclusive: an inline list replaces
the config file's list. Use one mode or the other.
```

It refuses because an inline list *replaces* the file's list rather than
extending it. Accepting both would silently discard the builds you wrote down.

**`cache-token` is ignored in config mode.** It names a token for the single
`cache` input, which does not exist here. Supply one environment variable per
registry instead, named `AEROFLARE_TOKEN_<HOST>` with `.` and `:` replaced by
`_`. Above, `docker.io` needs `AEROFLARE_TOKEN_DOCKER_IO`; `ghcr.io` needs
nothing, because of its `GITHUB_TOKEN` fallback.

**Cache order matters.** The first entry is the primary: it backs the substituter
used during the build, and its token is validated before anything is built.

### Editor support

The `yaml-language-server` comment on line one gives you completion and
validation in any editor with a YAML language server. The schema is strict —
`additionalProperties: false` — so a typo like `upstream_cache` is flagged as
an unknown key rather than silently ignored.

The schema also rejects `upstream-cache: []`, which the binary alone would read
as "unset" and quietly replace with the default. Write `upstream-cache: none` if
you mean no filtering.

Every key, its type, and its default is listed in the
[CI Configuration Schema](../reference/ci-configuration.md).

## GitLab CI

There is no `gh` CLI and no attestation flow outside GitHub, so install
`aeroflare-ci` from the flake instead. It is one of the binaries in the default
package.

```yaml title=".gitlab-ci.yml"
cache-nix:
  image: nixos/nix:2.24.9
  variables:
    NIX_CONFIG: "experimental-features = nix-command flakes"
    AEROFLARE_GIT_USERNAME: gitlab-ci-token
  script:
    - |
      host=$(printf '%s' "$CI_REGISTRY" | tr '[:lower:]' '[:upper:]' | tr '.:' '__')
      export "AEROFLARE_TOKEN_${host}=$CI_JOB_TOKEN"
    - nix shell github:ItzEmoji/aeroflare/v1.7.0 --command aeroflare-ci --config .aeroflare-ci.yaml
```

```yaml title=".aeroflare-ci.yaml"
builds:
  - .#default
caches:
  - registry.gitlab.com;my-group/my-project
```

The two GitLab-specific details:

**`AEROFLARE_GIT_USERNAME: gitlab-ci-token`.** A GitLab job token is not a
recognised PAT prefix, so without a username Aeroflare would send it straight
through as a Bearer token and the registry would reject it. Setting the username
sends it through the Basic-auth token exchange instead, which is what GitLab's
registry expects. A `glpat-` personal access token is recognised on its own and
does not need this.

**Deriving the token variable name.** `AEROFLARE_TOKEN_<HOST>` is a static name,
but `$CI_REGISTRY` is only known at run time, so build the name in the script.
The `tr` pipeline mirrors Aeroflare's own transformation: uppercase, then `.`
and `:` to `_`. A self-hosted registry on a port — `registry.example.com:5050` —
becomes `AEROFLARE_TOKEN_REGISTRY_EXAMPLE_COM_5050`.

Running as `root` in the `nixos/nix` image makes you a trusted user, so the
substituter takes effect.

## Any other CI system

The binary needs no config file at all. Everything except `workers` has an
environment variable, and list values accept newline- or comma-separated entries:

```bash
export AEROFLARE_CI_BUILDS='.#default,.#packages.x86_64-linux.foo'
export AEROFLARE_CI_CACHES='ghcr.io;me/nix-cache'
export AEROFLARE_CI_UPSTREAM_CACHE='https://cache.nixos.org'
export AEROFLARE_CI_COMPRESSION=zstd
export AEROFLARE_CI_SIGNING_KEY=NIX_SIGNING_KEY
export AEROFLARE_TOKEN_GHCR_IO="$MY_TOKEN"

aeroflare-ci
```

This is the whole contract. Jenkins, Woodpecker, Drone, Buildkite, a cron job on
a build server, or your laptop are all the same case.

To install without Nix, download the release archive directly:

```bash
version=1.8.0   # release archives are published from v1.8.0 onward
arch=x86_64     # or aarch64
curl -fsSL -o aeroflare-ci.tar.zst \
  "https://github.com/ItzEmoji/aeroflare/releases/download/v${version}/aeroflare-ci-${arch}.tar.zst"
tar --zstd -xf aeroflare-ci.tar.zst
./aeroflare-ci --config .aeroflare-ci.yaml
```

:::caution
Only releases from `v1.8.0` onward attach `aeroflare-ci-<arch>.tar.zst` archives,
and only for Linux. Earlier tags — including `v1.7.0` — carry source but no
assets, so this download will 404 against them. The flake path above has no such
restriction: it builds from the tag and works on any platform Nix supports.
:::

## Troubleshooting

**`✗ no token for primary cache <cache> (set AEROFLARE_TOKEN_<HOST>)`**
The first cache in the list has no token, and the run aborts before building.
Check the variable name: uppercase, `.` and `:` become `_`.

**A push fails for one cache but the run continues.**
Expected. Only the primary cache's token is checked up front. Other caches fail
individually, remaining caches still receive artifacts, and the process exits
`1`.

**`401 Unauthorized` on a non-GitHub registry.**
The token was probably sent as a Bearer token when the registry wanted Basic
credentials. Set `AEROFLARE_GIT_USERNAME` to the registry's expected username to
force the token exchange.

**The build rebuilds everything even though the cache is populated.**
The Nix daemon is ignoring `extra-substituters` because the build user is not
trusted. Add the user to `trusted-users` in `nix.conf`, or run as root.

**`builds contains "…", which is the "…" action input`**
A YAML indentation mistake under `builds: |`. Dedent the input.

**Only one installable is built despite a config file listing several.**
An inline `--build` or `builds:` input replaced the file's list. Lists replace;
they never merge.

**`release <tag> of <repo> ships no aeroflare-ci-<arch>.tar.zst`**
The Action resolves the release tag from the `version.json` at the ref you
pinned, then downloads that release's archive. Tags before `v1.8.0` publish no
archives. Pin the Action to a release that does.

## Related

- [The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md) — resolution, tokens, exit codes
- [Incremental Caching](../explanation/incremental-caching.md) — what gets skipped on a re-run
- [Authentication & Authorization](./authentication.md) — credentials for the interactive CLI
