---
sidebar_position: 4
title: The aeroflare-ci Runner
---

# The `aeroflare-ci` Runner

`aeroflare-ci` is a second binary, distinct from the interactive `aeroflare`
CLI. It is non-interactive, single-shot, and does exactly one thing: build a set
of Nix flake installables and push the results to one or more OCI caches.

It knows nothing about GitHub. The GitHub Action is a thin wrapper that
downloads this binary and translates action inputs into flags. Every capability
the Action exposes is therefore reachable from any CI system, or from your
laptop — see [GitHub Action](../how-to/github-action.md) for the Action itself,
or [CI Integration](../how-to/ci-integration.md) for GitLab CI and generic
runners.

## The pipeline

A single run performs five stages in order.

1. **Resolve.** Merge the config file with flags and environment, apply
   defaults, and validate. Nothing has happened yet; a bad config fails here.
2. **Substituter.** Start a local proxy on `127.0.0.1` that presents the
   *primary* cache and every upstream to Nix as a binary cache.
3. **Build.** Run `nix build <installable> --print-out-paths` once per entry,
   with `extra-substituters` pointed at the proxy. Store paths are scraped from
   stdout and deduplicated across installables.
4. **Filter and prepare.** Tear the proxy down, drop the outputs and references
   an upstream already serves, and archive what remains into NAR blobs exactly
   once — regardless of how many caches will receive them.
5. **Push.** Upload the prepared set to every cache in turn.

Two consequences fall out of this shape. The prepared set is built once and
reused, so adding a second cache costs upload bandwidth but no extra
compression. And **every build is pushed to every cache**; there is no way to
route one installable to one cache and another elsewhere.

For what stages 4 and 5 skip, see [Incremental Caching](./incremental-caching.md).

## The primary cache

The **first** entry in `caches` is the primary. It is not merely first among
equals:

- It backs the substituter in stage 2, so builds are accelerated by the primary
  cache's contents and not by the others'.
- Its token is resolved *before any build runs*. If it is missing, the run
  aborts immediately rather than building for several minutes and then failing
  to push.

A missing token for any *other* cache is not fatal. That cache is skipped, the
push is recorded as failed, remaining caches still receive the artifacts, and
the process exits non-zero.

Cache order is therefore meaningful. Put the cache you build against most often
first.

## Configuration resolution

Three sources feed one `RunSpec`, in descending precedence:

1. Command-line flags
2. Environment variables
3. The config file
4. Built-in defaults

The rule that catches people is what happens to lists.

:::danger Lists replace, they do not merge
An inline `builds`, `caches`, or `upstream-cache` **replaces** the config file's
list wholesale. It never appends. Passing `--build .#foo` alongside a config
file that lists three installables builds exactly one.

This is why the GitHub Action refuses `config` together with `builds`/`cache`
rather than quietly discarding the file's values.
:::

### Where each setting comes from

| Setting | Flag | Environment variable | Config key | Default |
|---|---|---|---|---|
| Installables | `--build` (repeatable) | `AEROFLARE_CI_BUILDS` | `builds` | — (required) |
| Push targets | `--cache` (repeatable) | `AEROFLARE_CI_CACHES` | `caches` | — (required) |
| Config path | `--config` | `AEROFLARE_CI_CONFIG` | — | `.aeroflare-ci.yaml` |
| Compression | `--compression` | `AEROFLARE_CI_COMPRESSION` | `compression` | `zstd` |
| Signing key | `--signing-key` | `AEROFLARE_CI_SIGNING_KEY` | `signing-key` | unsigned |
| Upstream caches | `--upstream-cache` (repeatable) | `AEROFLARE_CI_UPSTREAM_CACHE` | `upstream-cache` | `https://cache.nixos.org` |
| Upload workers | `--workers` | — | `workers` | `50` |

List-valued environment variables accept newline- **or** comma-separated
entries, trimmed, with blanks discarded. `AEROFLARE_CI_BUILDS=".#a,.#b"` and a
two-line value are equivalent.

:::note
`workers` is the one setting with no environment variable. In an
environment-only deployment it can only be set via `--workers` or the config
file.
:::

### The config file is optional, unless you name it

`aeroflare-ci` always looks for `.aeroflare-ci.yaml` in the working directory.
If it is absent, that is not an error — the run proceeds on flags and
environment alone.

Naming a *different* path makes the file mandatory, and a missing one is fatal:

```console
$ aeroflare-ci --config /nonexistent.yaml
aeroflare-ci: open /nonexistent.yaml: no such file or directory
$ echo $?
1
```

Passing `--config .aeroflare-ci.yaml` explicitly is still treated as the default
path, and so remains optional. The check compares the resolved path against the
default string, not against whether the flag was supplied.

## Token resolution

Push tokens are read from the environment only. There is no flag, and no token
ever appears in a config file.

For a registry host, the variable name is the host uppercased with `.` and `:`
replaced by `_`:

| Registry | Environment variable |
|---|---|
| `ghcr.io` | `AEROFLARE_TOKEN_GHCR_IO` |
| `docker.io` | `AEROFLARE_TOKEN_DOCKER_IO` |
| `registry.gitlab.com` | `AEROFLARE_TOKEN_REGISTRY_GITLAB_COM` |
| `localhost:5000` | `AEROFLARE_TOKEN_LOCALHOST_5000` |

`ghcr.io` alone has a fallback: if `AEROFLARE_TOKEN_GHCR_IO` is unset,
`GITHUB_TOKEN` is used. No other host has one.

```console
$ AEROFLARE_CI_BUILDS='.#default' AEROFLARE_CI_CACHES='ghcr.io;me/c' aeroflare-ci
aeroflare-ci: 1 builds, 1 caches
✗ no token for primary cache ghcr.io;me/c (set AEROFLARE_TOKEN_GHCR_IO)
```

### How the token is presented to the registry

The token is a **password**, and it is always presented as one. Aeroflare does
not inspect its shape, and there is no classification step: it hands the
credential to the registry over Basic auth, and the registry hands back the
short-lived Bearer token it wants to see on subsequent requests.

That exchange is the standard Docker Registry v2 token flow, and Aeroflare does
not implement it — [go-containerregistry][gcr] does. It pings `/v2/`, reads the
`WWW-Authenticate` challenge to discover the realm and service (which need not
be on the registry's own host: Docker Hub challenges `registry-1.docker.io`
requests to a realm on `auth.docker.io`), requests the scopes the operation
needs, and re-authenticates whenever the registry says the token has expired.
A push large enough to outlive a token therefore still finishes.

So a GitHub PAT (`ghp_`), an Actions token (`ghs_`), a GitLab job token, a
Docker Hub PAT (`dckr_pat_`) and a self-hosted Harbor password all take exactly
the same path. Any registry implementing the standard token flow works, with no
per-registry code.

The username is read from `AEROFLARE_USERNAME_<HOST>`, falling back to
`AEROFLARE_GIT_USERNAME` and then to `token`. It matters only for registries
that check it: GitLab expects `gitlab-ci-token` for a job token and Docker Hub
expects the real account name, while `ghcr.io` ignores it entirely.

[gcr]: https://github.com/google/go-containerregistry

## Signing key resolution

The `signing-key` setting is overloaded, and the order of interpretation is:

1. If the value **names an environment variable that is set and non-empty**, the
   contents of that variable are written to a `0600` temporary file, used, and
   removed when the run ends.
2. Otherwise the value is treated as a **filesystem path**.
3. If it is neither, the run fails: `signing key "…" is neither a set env var nor
   an existing file`.

So `signing-key: NIX_SIGNING_KEY` reads the key material from
`$NIX_SIGNING_KEY`, while `signing-key: ./key.sec` reads the file. The env var
form is what you want in CI: the key never touches the working directory, and
the temp file is unreadable by other users.

Omit the setting entirely and NARs are pushed unsigned.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Every build and every push succeeded. |
| `1` | Configuration error, or at least one build or push failed. |
| `2` | Flag parsing failed, including `-h`. |

A partial failure — three of four caches pushed — is exit `1`. The run does not
abort on the first failure; it completes what it can and reports the tally.

## Runtime requirements

- **`nix` on `PATH`.** `aeroflare-ci` shells out to `nix build` and
  `nix-store --dump`. It does not install Nix.
- **Flakes enabled**, since installables are flake references.
- **A trusted user.** The substituter is injected via `NIX_CONFIG` as
  `extra-substituters`. The Nix daemon ignores that setting for untrusted users,
  and the build silently falls back to rebuilding rather than substituting. It
  still succeeds — just slowly.
- The binary deliberately never sets `accept-flake-config`, which would trust
  substituters and public keys declared by an arbitrary flake.

The published release archives are Linux `x86_64` and `aarch64` only. Building
from source through the flake works anywhere Go and Nix do; `aeroflare-ci` is
one of the binaries in the default package's `bin/`.

## Related

- [GitHub Action](../how-to/github-action.md) — the Action itself
- [CI Integration](../how-to/ci-integration.md) — GitLab CI and generic runners
- [Incremental Caching](./incremental-caching.md) — what gets skipped, and why
- [Architecture & Design](./architecture.md) — the wider system
