---
sidebar_position: 3
title: Repository Layout & Codebase
---

# Repository Layout & Codebase Walkthrough

Aeroflare is a Go module split along one line that matters: **`pkg/` is the
importable engine, `internal/` is everything that only makes sense inside the
CLI.** The Go toolchain enforces the second half of that (no external module can
import `internal/`), and `make check-api` enforces the first half by failing if an
`internal/` type leaks into a `pkg/` signature.

If you intend to *use* Aeroflare as a library rather than modify it, read the
[Go API](./go-api.md) page instead — this one is about finding your way around
the source.

## `cmd/` — binaries

| Path | What it is |
|---|---|
| `cmd/aeroflare/` | The interactive CLI. `main.go` only. |
| `cmd/aeroflare-ci/` | The non-interactive CI runner, which the GitHub Action wraps. |
| `cmd/gen_docs/` | Generates the CLI reference pages under `docs/docs/reference/cli/` from the Cobra tree. |

## `pkg/` — the engines

The four library packages, in dependency order. None of them read a config file,
an environment variable, or the keychain; none of them write to stdout. Registry,
repository, and credential are always parameters.

* **`pkg/prepare/`** — turning a store path into artifacts. Sub-packages: `store`
  (querying the Nix store), `hash`, `compress` (zstd/xz/gzip), `narinfo`
  (generating and serialising the metadata), `signing`, `cache`, and `prepare`
  itself.
* **`pkg/oci/`** — the registry layer, and the most load-bearing package in the
  project. `network.go` streams `.nar` blobs as OCI layers and maps `.narinfo`
  fields onto manifest annotations (`vnd.aeroflare.nar.*`) using
  `google/go-containerregistry`. `oci.go` parses those annotations back.
  `auth.go` builds credentials — and *only* builds them: the token exchange, the
  retry policy, and re-authentication on expiry are all delegated to
  go-containerregistry's transport rather than reimplemented.
  `config_manifest.go` reads and writes the `cache-config` manifest.
* **`pkg/push/`** — the push pipeline. Prepares each path, filters out what the
  registry or the upstream cache already has, uploads the rest in chunks, and
  flushes receipts after each chunk so an interrupted push keeps what it
  uploaded. A per-path failure is collected, not fatal. Progress goes through a
  `Reporter` the caller supplies.
* **`pkg/proxy/`** — the substituter. `proxy_server.go` answers
  `/nix-cache-info`, `/<hash>.narinfo`, `/nar/<…>`, and `/public-key`, resolving
  each narinfo from manifest annotations and streaming each NAR straight from the
  registry blob without buffering to disk. `bootstrap.go` starts it and resolves
  cache-wide config. Requests the registry cannot satisfy fall through to the
  configured upstreams.

Supporting packages: **`pkg/cmd/*`** holds the Cobra command constructors (one
sub-package per command, each thin: parse flags, resolve config, call an engine);
**`pkg/cmdutil`** is the `Factory` plus registry/credential resolution — the
worked example of how to feed the engines; **`pkg/iostreams`** abstracts
stdin/stdout/stderr for testability.

## `internal/` — CLI-only logic

* `internal/aerocmd/` — assembles the command tree and the factory.
* `internal/auth/` — token resolution and validation. The `Resolver` walks flags →
  environment (`GITHUB_TOKEN`, `GH_TOKEN`, …) → secrets manager, in that order,
  and validates that a PAT actually carries `write:packages`.
* `internal/secrets/` — token storage, backed by the OS keychain via
  `zalando/go-keyring`.
* `internal/backend/` — the `CacheBackend` abstraction and its `NativeBackend`
  implementation, which publishes each completed push as its own OCI image.
* `internal/run/` — the `aeroflare run` wrapper: spawn an ephemeral proxy on a
  free port, inject `--option extra-substituters`, run the subprocess, push what
  it printed.
* `internal/ci/` — the `aeroflare-ci` runner: config parsing, cache specs, build
  filtering, signing-key resolution, reporting.
* `internal/init/` — the provisioning wizard (GitHub/GitLab repo creation,
  Cloudflare Worker deployment) and the `huh` theme.
* `internal/ui/` — terminal output primitives (boxes, tables). Deliberately
  unreachable from the engines.
* `internal/build/` — `Version` and `Date`, injected at link time.

## How data flows through a push

Tracing `aeroflare push --store-path /nix/store/abc…-package`:

1. **Entry.** `pkg/cmd/push` parses the flags. `pkg/cmdutil` resolves the registry,
   the repository, and a credential, and builds an `authn.Authenticator`.
2. **Preflight.** `pkg/push` decides what actually needs uploading, dropping paths
   the registry or the upstream cache already serves.
3. **Prepare.** `pkg/prepare` serialises the store path into a `.nar`, compresses
   it (zstd by default), hashes it, and builds the `.narinfo` in memory — signing
   it if a key was supplied.
4. **Upload blob.** `pkg/oci` streams the compressed NAR to the registry as a raw
   `v1.Layer`.
5. **Map metadata.** `pkg/oci` builds an OCI manifest and writes every narinfo
   field (`StorePath`, `FileHash`, `NarHash`, `Sig`, …) into `vnd.aeroflare.nar.*`
   annotations on it.
6. **Tag.** The manifest is tagged with the 32-character Nix store hash (`abc…`).
   That tag *is* the index: a later `<hash>.narinfo` request becomes a single
   manifest fetch, with no database to consult.
