---
id: maintenance
title: Maintenance & Utils
sidebar_position: 4
---

# Maintenance & Utils

Aeroflare provides a suite of utility commands for interacting directly with blobs and preparing store paths. These commands are useful for operators directly manipulating the cache states and for developers extending Aeroflare's core workflows.

## `push-blob` and `pull-blob`

These lower-level utilities offer direct OCI blob interactions. They are heavily utilized for manual state debugging or bespoke cache layer manipulations without relying on Nix commands.

**Usage:**
```bash
aeroflare push-blob <file-path>
aeroflare pull-blob <digest> <output-file>
```

### Technical Mechanics

- **`push-blob`**: Resolves the target registry and repository configuration, grabs the appropriate token, and wraps `network.PushBlob(filePath, ...)`. It streams the given file payload directly to the OCI registry as a single blob and returns its final computed digest.
- **`pull-blob`**: Wraps `network.PullBlob(digest, outFile, ...)`. It queries the registry for the specified blob digest and streams the bytes down, writing them locally to `outFile`.

## `prepare`

The `prepare` command is essential for processing Nix store paths into valid NAR archives and corresponding `narinfo` metadata prior to registry submission.

**Usage:**
```bash
aeroflare prepare --store-path <path> [flags]
aeroflare prepare --input <file> [flags]
```

**Flags:**
- `--store-path` / `--input`: Pass a single store path or a batch input file.
- `--output-dir` (default: `./output`): Directory for generated `.nar` and `.narinfo` assets.
- `--compression` (default: `zstd`): Formats supported via `compress.ParseType()` (zstd, xz, gzip, none).
- `--workers` (default: `50`): Parallelization limit for batch workloads.
- `--prepare-refs`: Enable recursive preparation of missing references one-level deep.
- `--signing-key`: Provide an ed25519 signing key generated via `nix key-gen-secret`.
- `--upstream-cache` (default: `https://cache.nixos.org`): The upstream cache for evaluating missing store references.

### Technical Mechanics

1. **Input & Config:** Constructs a `prepare.Config` populated with parsed compression routines, the parsed `signing.PrivateKey`, and concurrency parameters.
2. **Processing Pipelines:** 
   - Single targets evaluate through `prepare.Prepare()`.
   - Batch targets utilize `prepare.PrepareBatch()`, dispatching to a worker pool bound by the `--workers` flag.
3. **Reference Tracking:** It evaluates `.narinfo` files against the configured `--upstream-cache`. If `--prepare-refs` is true, the resolver fetches missing downstream dependency boundaries (one level deep) and prepares them concurrently. The results compile into a `prepare.Result` struct, detailing `MissingRefs`, `MissingRefResults`, and boolean signature status (`Signed`).
