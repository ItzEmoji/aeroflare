---
sidebar_position: 6
title: Signing Keys
---

# Signing Keys

Nix will not use a substituted path unless it is signed by a key the client
trusts. An unsigned Aeroflare cache is perfectly usable for storing artifacts,
but every machine that tries to *consume* it will download the NAR and then
refuse to install it. Signing is what closes that loop.

There are three steps, and all three are needed: **generate** a key pair,
**sign** on push, and **trust** the public half on every consuming machine.

## 1. Generate a key pair

```bash
nix key generate-secret --key-name my-cache-1 > /path/to/cache-priv-key.pem
nix key convert-secret-to-public < /path/to/cache-priv-key.pem
```

The key name is arbitrary but should be unique to the cache; by convention it
carries a trailing number so the key can be rotated later (`my-cache-1`,
`my-cache-2`). The public key is printed as `my-cache-1:AbCd...=` — that whole
string, name included, is what clients trust.

Keep the private key secret. Anyone holding it can sign artifacts that your
machines will install without question.

## 2. Sign on push

Pass the private key to whichever command uploads:

```bash
aeroflare push ./result --signing-key /path/to/cache-priv-key.pem
aeroflare run --signing-key /path/to/cache-priv-key.pem -- nix build .#default --print-out-paths
```

`aeroflare prepare` takes the same flag, if you are generating artifacts to
upload separately.

Without `--signing-key`, artifacts are pushed unsigned. Nothing fails at push
time — the consequence only appears on the machine trying to use the cache.

## 3. Publish the public key on the cache

Store the public key on the cache itself, so the proxy can serve it and clients
can discover it:

```bash
aeroflare configure
```

This interactive command prompts for the public key and writes it to the
`aeroflare.public-key` annotation on the cache's `cache-config` manifest. It
prefills the current value, so re-running it is also how you inspect what is set.

The proxy exposes the result at `/public-key`, which is a quick way to confirm it
took:

```bash
curl http://127.0.0.1:8080/public-key
```

## 4. Trust the key on consuming machines

Add the public key to `trusted-public-keys` in `nix.conf`:

```
trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= my-cache-1:AbCd...=
```

The list is space-separated. Keep `cache.nixos.org-1` — dropping it breaks
substitution from upstream.

On NixOS:

```nix
nix.settings.trusted-public-keys = [
  "my-cache-1:AbCd...="
];
```

## Signing in CI

The CI runner takes the same setting through `.aeroflare-ci.yaml`, but with one
extra convenience: `signing-key` may be **either a filesystem path or the name of
an environment variable holding the key material.**

```yaml
signing-key: NIX_SIGNING_KEY   # the NAME of an env var, not a path
```

If `$NIX_SIGNING_KEY` is set, its contents are written to a temporary file with
mode `0600` and removed when the run ends. This is what lets you keep the key in
a GitHub Actions secret and never write it to the workspace. If the value is not
a set environment variable, it is treated as a path, and the run fails early if
no such file exists.

See [CI Configuration](../reference/ci-configuration.md) and the
[GitHub Action](./github-action.md) guide.
