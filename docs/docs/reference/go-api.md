---
sidebar_position: 5
title: Go API
---

# Go API

Aeroflare is a Go module as well as a CLI. The engines that push to and serve
from an OCI registry live under `pkg/` and can be imported.

```
github.com/itzemoji/aeroflare
```

📦 **[pkg.go.dev/github.com/itzemoji/aeroflare](https://pkg.go.dev/github.com/itzemoji/aeroflare)**

## Stability

:::caution The `pkg/` API is not covered by semver.

Aeroflare follows the [`gh` CLI](https://github.com/cli/cli) model: the exported
Go API may change between releases, including in patch releases, without a major
version bump. It exists because the engines are genuinely reusable, not because
we are promising an interface.

Import it at your own risk, and pin a version. If you need a stable contract,
shell out to the CLI instead.
:::

## Packages

| Package | Purpose |
|---|---|
| [`pkg/oci`](https://pkg.go.dev/github.com/itzemoji/aeroflare/pkg/oci) | Registry client. Pushes NARs as layers, maps narinfo onto manifest annotations, reads and writes the `cache-config` manifest, and builds credentials. |
| [`pkg/push`](https://pkg.go.dev/github.com/itzemoji/aeroflare/pkg/push) | The push pipeline: store paths → NAR + narinfo → registry, with upstream filtering, chunked uploads, and resumable receipts. |
| [`pkg/proxy`](https://pkg.go.dev/github.com/itzemoji/aeroflare/pkg/proxy) | An embeddable Nix substituter. Serves `/nix-cache-info`, `/<hash>.narinfo`, and `/nar/<…>` straight from a registry, holding no local state. |
| [`pkg/prepare`](https://pkg.go.dev/github.com/itzemoji/aeroflare/pkg/prepare) | NAR serialisation, hashing, compression, narinfo generation, and signing. |

Three design rules hold across all four, and are worth knowing before you import
any of them:

- **They read no configuration.** No config file, no environment variable, no
  keychain. Registry, repository, and credential are always explicit parameters.
  Resolving those is the caller's job; the CLI's own resolution in `pkg/cmdutil`
  is a worked example.
- **They write nothing to stdout.** Progress and failures are delivered through a
  `Reporter` interface you supply, so an embedding program owns its own output.
- **They do no token bookkeeping.** A credential is an `authn.Authenticator`. The
  registry exchange, and the refresh when the exchanged token expires, happen
  inside the HTTP transport — so a push long enough to outlive a token still
  finishes.

## Example

Pushing a store path, with output suppressed:

```go
import (
    "github.com/google/go-containerregistry/pkg/authn"

    "github.com/itzemoji/aeroflare/pkg/oci"
    "github.com/itzemoji/aeroflare/pkg/push"
)

plan, err := push.Preflight(&push.PushConfig{
    TargetPaths: []string{"/nix/store/0nlp2xwzavr9dyrsdhcgnq2h4qxsi8bp-hello-2.12.1"},
    Compression: "zstd",
    Workers:     50,
    PrepareRefs: true,
    CacheURL:    "https://cache.nixos.org", // paths this already serves are skipped
})
if err != nil {
    return err
}

target := push.Target{
    Registry:   "ghcr.io",
    Repository: "my-org/nix-cache",
    Auth:       oci.PasswordAuth("my-org", token), // the registry exchanges it
}

result, err := push.RunPushTo(plan, target, silentReporter{})
```

Runnable versions of this live in the repository as `Example` functions
(`pkg/push/example_test.go`, `pkg/proxy/example_test.go`, `pkg/oci/example_test.go`)
and are rendered on pkg.go.dev.

## What is *not* importable

Everything under `internal/` — the wizard, the secrets keychain, the terminal UI,
the CI runner — is closed to external modules by the Go toolchain, deliberately.

That boundary is enforced mechanically, not just by convention. `make check-api`
fails the build if an `internal/` type appears in the public signature of a
library package (where an external caller could never name it), or if one of the
engines takes a dependency on `internal/ui`. See
[Development](../contributing/development.md).
