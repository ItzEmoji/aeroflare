---
sidebar_position: 3
title: Incremental Caching
---

# Incremental Caching

A question that comes up on every second run of a cache job: if some of these
dependencies are already cached, does Aeroflare re-upload everything, upload
nothing, or upload only what is missing?

**It uploads only what is missing.** Nothing is skipped that a cache does not
already hold, and nothing already held is transferred a second time.

Getting there involves four independent mechanisms, and they filter against two
different notions of "already cached". Confusing the two is the usual source of
surprise, so it is worth separating them up front.

## Two kinds of "already cached"

| | Meaning | Configured by |
|---|---|---|
| **Upstream cache** | A public cache whose paths you do not want to duplicate, such as `https://cache.nixos.org`. | `upstream-cache` |
| **Destination cache** | The OCI registry Aeroflare pushes to, such as `ghcr.io/you/nix-cache`. | `cache` or `caches` |

Aeroflare's *filters* consult the upstream cache. Aeroflare's *uploader* consults
the destination cache. Both are always active, and they eliminate different work.

## The four mechanisms

### 1. Substitution during build

Before building, Aeroflare starts a local proxy that presents the primary
destination cache — plus every upstream — to Nix as a binary cache substituter.
A path that already exists in either is fetched rather than rebuilt.

This is the largest saving and the one you notice first: the second run of an
unchanged job builds almost nothing.

### 2. Root filtering

Nix build outputs are the *roots* of the closure. `filterRoots` in the `ci`
package queries each upstream cache for the roots' store path hashes and drops
those the upstream already serves.

This step exists for a specific reason: preparation filters a path's
*references* but never the path itself. Without root filtering, a build output
that lives on `cache.nixos.org` would be re-uploaded on every single run.

A root whose hash cannot be parsed is kept rather than dropped. Re-uploading a
path that cannot be classified is merely wasteful; dropping it would be wrong.

### 3. Reference filtering

The `prepare` stage walks the transitive closure of each surviving root and
drops every reference an upstream already serves. What remains is the closure
minus upstream — the set Aeroflare will actually turn into NAR archives.

The run log names this set explicitly:

```
prepare  47 store paths (closure minus upstream)
```

With `upstream-cache: none` no filtering happens at all, and the log instead
reads `(full closure)`, because the cache is being made self-contained.

### 4. Blob deduplication at the registry

The surviving paths are compressed into NAR blobs and uploaded. Before sending
any bytes, the OCI client issues a `HEAD` request for the blob's digest and
skips the transfer entirely when the registry already stores it.

This is the mechanism that covers the destination cache, and it is what makes a
re-run of an identical closure cheap even though the earlier filters never
consulted the destination at all.

It works because Aeroflare's compression is byte-deterministic. The same NAR
compressed with the same algorithm produces the same blob digest on every
machine, independent of core count. Were that not true, every path would present
a novel digest and re-upload on each run.

:::note
Blob digests are stable for a given Aeroflare release. Upgrading the compression
library can change them, in which case the affected blobs upload once more and
are then stable again.
:::

## What this means in practice

For a typical second run, where the flake is unchanged:

1. Every path is substituted from the cache rather than rebuilt.
2. The roots already on `cache.nixos.org` are dropped.
3. Their nixpkgs dependencies are dropped.
4. Whatever remains is compressed, `HEAD`-checked, found present, and not sent.

The result is a job that transfers no blob data. Change one package and only
that path — and the paths downstream of it — are prepared and uploaded.

## Rough edges

Two behaviours are worth knowing about, because they are visible in the logs and
neither is what a first reading suggests.

**The pushed count overstates transfers.** The `(N pushed)` figure in

```
✓ push    → ghcr.io;you/nix-cache   (47 pushed)
```

counts every path the uploader *processed*, not every path it transferred. A
path whose blob the registry already held is counted here alongside one that was
genuinely sent. In a no-op run the figure equals the prepared path count, and
zero bytes crossed the network. Read it as "paths considered", not "paths
uploaded".

**Paths in your cache but not upstream are recompressed.** Filtering happens
against upstream caches only. A path that lives in your destination cache but
not on `cache.nixos.org` — anything you built yourself — is re-archived,
recompressed, rehashed and resigned on every run, and is discarded only at the
final `HEAD` check. The upload is correctly skipped; the CPU work is not.

The `aeroflare push` CLI does not share this behaviour. It queries the
destination cache directly and skips such paths before preparing them.

## Related

- [Cache Population](../how-to/cache-population.md) — populating a cache by hand
- [OCI Integration Protocol](./oci-integration.md) — how NARs map onto registry blobs
