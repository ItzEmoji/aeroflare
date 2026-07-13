---
sidebar_position: 1
title: Development
---

# Development

## Prerequisites

Go, at the version in `go.mod`. A Nix installation is needed to run the tests
that touch a real store, and to build the flake.

## Common tasks

The repository carries both a `Taskfile.yml` ([Task](https://taskfile.dev)) and a
`Makefile`. Task drives the day-to-day build; the Makefile carries the checks CI
runs.

```bash
task              # default target
task build        # build the aeroflare binary
task build-ci     # build the aeroflare-ci binary
task build-all    # both
task test
task lint
task install      # build and install into your PATH
task dist         # release archives
task clean
task help         # list every target with its description
```

Most targets have `-ci` and `-all` variants, selecting the `aeroflare-ci` binary
or both binaries respectively.

## Checks

```bash
make check-api
```

This guards the [public Go API](../reference/go-api.md) with two invariants the
compiler cannot express:

1. **No `internal/` type may appear in the public signature of a `pkg/` library
   package.** A `pkg/` package is allowed to *import* `internal/` — that
   restriction only binds code outside the module — so such a leak compiles
   cleanly here while being unusable for the external callers the package was
   promoted for. Only a doc-level check catches it. It is fatal for the library
   packages (`oci`, `push`, `proxy`, `prepare`, `cmdutil`, `iostreams`) and a
   warning for `pkg/cmd/**`, whose real API is just `NewCmdX`.
2. **The engines must not depend on `internal/ui`.** A library does not write to
   its caller's stdout; presentation belongs in the command layer.

The implementation is `scripts/check_api_leaks.sh`. Without these gates, the
first convenience function someone adds quietly undoes the decoupling.

## Regenerating the CLI reference

The pages under `docs/docs/reference/cli/` are **generated** from the Cobra
command tree — do not edit them by hand; the next regeneration will overwrite
your changes.

```bash
go run ./cmd/gen_docs
```

The generator escapes angle brackets in prose (so a placeholder like `<token>` in
help text is not parsed as a JSX tag, which would break the Docusaurus build) but
leaves fenced code blocks alone.

If you add or change a command's flags or help text, regenerate and commit the
result alongside the code change. If you add a whole command, also add it to the
CLI category in `docs/sidebars.ts` — the sidebar list is not generated.

## Versioning

`internal/build` holds `Version` and `Date`, injected at link time:

```
-ldflags "-X github.com/itzemoji/aeroflare/internal/build.Version=v1.8.0 ..."
```

computed by `scripts/build.go` and applied by the Makefile, the release workflow,
and `default.nix`.

A plain `go build` supplies no ldflags, so `Version` stays at its `"dev"`
default — but an `init()` then falls back to the module version from
`debug.ReadBuildInfo()`. That is why `go install …@latest` still reports a real
version, while a build from a dirty working tree reports a pseudo-version like
`v1.7.1-0.20260713071150-4f16fc94a9bf+dirty`.

## Docs site

The site is [Docusaurus](https://docusaurus.io/), in `docs/`.

```bash
cd docs
yarn         # install
yarn start   # dev server with live reload
yarn build   # production build — also catches broken internal links
```

Run `yarn build` before submitting docs changes: a link to a page that does not
exist, or a sidebar entry pointing at a missing doc id, fails the build rather
than degrading silently.
