---
sidebar_position: 6
title: CI Integration
---

# CI Integration

`aeroflare-ci` is a plain binary driven by flags, environment variables, and an
optional YAML file. The [GitHub Action](./github-action.md) is a convenience
wrapper around it, not a privileged path — anything it does, another CI system
can do too.

This guide covers GitLab CI and runners with no integration at all. For the
GitHub Action itself — configless mode, `config:` mode, and its own
troubleshooting — see [GitHub Action](./github-action.md).

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

## GitHub Actions

Configless mode, `config:` mode, the `builds: |` indentation trap, and
provenance verification all live on the dedicated
[GitHub Action](./github-action.md) page.

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

**`AEROFLARE_GIT_USERNAME: gitlab-ci-token`.** Aeroflare presents every
credential to the registry as a password, over Basic auth, and GitLab checks the
username that comes with it: a job token must be paired with `gitlab-ci-token`.
Without it the username defaults to `token` and GitLab rejects the pair. A
`glpat-` personal access token is paired with your own username instead. You can
also set this per registry, as `AEROFLARE_USERNAME_<HOST>`, which takes
precedence.

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
Most likely the username. Aeroflare presents the token as a password over Basic
auth, defaulting the username to `token`; registries that check it will reject
that pair. Set `AEROFLARE_USERNAME_<HOST>` (or `AEROFLARE_GIT_USERNAME`) to the
username the registry expects — `gitlab-ci-token` for a GitLab job token, your
account name for Docker Hub.

**The build rebuilds everything even though the cache is populated.**
The Nix daemon is ignoring `extra-substituters` because the build user is not
trusted. Add the user to `trusted-users` in `nix.conf`, or run as root.

**Only one installable is built despite a config file listing several.**
An inline `--build` or `builds:` input replaced the file's list. Lists replace;
they never merge.

## Related

- [GitHub Action](./github-action.md) — the Action's configless and config modes
- [The `aeroflare-ci` Runner](../explanation/aeroflare-ci.md) — resolution, tokens, exit codes
- [Incremental Caching](../explanation/incremental-caching.md) — what gets skipped on a re-run
- [Authentication & Authorization](./authentication.md) — credentials for the interactive CLI
