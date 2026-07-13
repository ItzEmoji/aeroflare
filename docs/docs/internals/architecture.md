---
id: architecture
title: Core Architecture
sidebar_position: 1
---

# Core Architecture

This page is about the mechanics of `pkg/oci` — the package that turns Nix cache
semantics into OCI registry calls. It is for people changing that code, not using
it. (For the importable surface, see the [Go API](../reference/go-api.md).)

Two ideas carry the whole design:

- **The tag is the index.** A package's OCI image is tagged with its 32-character
  Nix store hash, so a narinfo lookup is one manifest fetch and there is no
  database to keep in sync.
- **Aeroflare implements no registry protocol.** Token exchange, retry, and
  re-authentication on expiry are all go-containerregistry's job. Aeroflare's job
  is only to say *which credential it holds*.

## The transport (`network.go`)

Every outgoing request rides a single shared `optimizedTransport`: an
`http.Transport` tuned for many concurrent blob uploads, wrapped in a
`loggingTransport`.

| Setting | Value |
|---|---|
| `ForceAttemptHTTP2` | `true` |
| `MaxIdleConns` | 1000 |
| `MaxIdleConnsPerHost` | 100 |
| `MaxConnsPerHost` | 100 |
| Dial timeout | 30s |
| `IdleConnTimeout` | 90s |
| `TLSHandshakeTimeout` | 10s |
| `ExpectContinueTimeout` | 1s |

`loggingTransport` implements `http.RoundTripper` and logs the method and URL of
every request when debug logging is on. That is an atomic flag toggled by
`SetDebugHTTP`, which the root command sets from `-vv`.

In `auth.go`, `Transport` composes this with `retryBase` — go-containerregistry's
`transport.NewRetry`, giving bounded exponential backoff on retryable status
codes — and then `transport.NewWithContext`, which performs the registry token
exchange and refreshes the token when it expires. Nothing above this layer tracks
token lifetimes, which is why a push that runs longer than a token's validity
still completes.

## Credentials (`auth.go`)

A credential is an `authn.Authenticator`, and there are exactly three:

- **`PasswordAuth(username, password)`** — a password or PAT, which the registry
  exchanges. Every credential the CLI resolves takes this path; nothing inspects a
  token's prefix to guess what it is. The username matters to registries that
  check it (Docker Hub) and is ignored by those that do not (ghcr.io); it defaults
  to `"token"` when empty.
- **`BearerAuth(token)`** — an already-exchanged token, sent verbatim.
- **`nil`** — anonymous, which is all a public cache needs to be read.

Supporting a registry other than ghcr.io is therefore a configuration question,
not a code change.

## Push (`network.go`)

- **`NewLayer` / `NewLayerFast`** wrap a local file as a `fileLayer` implementing
  `v1.Layer`. `NewLayerFast` takes the narinfo, so it reuses hashes already
  computed during prepare instead of re-digesting the file.
- **`PushBlob` / `PushLayer`** stream a layer to the registry via
  `remote.WriteLayer`.
- **`PushNarPackage`** is where the storage model is realised. It wraps the NAR
  layer into an OCI image (`types.OCIManifestSchema1`), writes each narinfo field
  into a `vnd.aeroflare.nar.*` manifest annotation (`storepath`, `filehash`,
  `narhash`, …), and **tags the image with the 32-character Nix hash** — the O(1)
  lookup rule. `PushNarPackageWith` does the same against a caller-supplied
  `remote.Pusher`, so a batch push reuses one authenticated pusher across many
  packages instead of re-authenticating per package.

## Read (`network.go`, `oci.go`)

**`PullOCINativeManifest`** fetches the image manifest by its 32-character tag and
reconstructs the `narinfo.Narinfo` entirely from the annotations — no blob is
fetched to answer a narinfo request. `PullBlob` streams the layer when the NAR
itself is wanted.

`oci.go` holds the annotation parsing (`ParseAeroflareMetadata`,
`FetchAeroflareAnnotations`) and `NewArtifactTypeImage`, a wrapper that stamps an
`artifactType` onto an image — needed because go-containerregistry's image builder
does not expose that field directly.

## Cache-wide config (`config_manifest.go`)

There is no central index, but there *is* one shared object: the `cache-config`
manifest. `PushConfigManifest` writes an empty OCI image (artifact type
`application/vnd.aeroflare.cache-config.v1`) whose annotations carry cache-wide
settings — currently `aeroflare.public-key`, which `aeroflare configure` writes
and the proxy reads to serve `/public-key`.

Package metadata never touches this manifest. It lives on each package's own
image.
