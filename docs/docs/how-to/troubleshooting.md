---
sidebar_position: 7
title: Troubleshooting
---

# Troubleshooting

Symptoms you are likely to hit, what causes them, and what to do about them.

## `401 Unauthorized` or `403 Forbidden` when pushing

The registry accepted the connection but refused the write. Almost always the
credential, not the code.

Check, in order:

1. **Scope.** A GitHub PAT needs `write:packages` to push to `ghcr.io`. Read-only
   tokens authenticate fine and then fail at the upload, which is why this looks
   like a bug rather than a permissions problem.
   ```bash
   aeroflare auth status
   ```
2. **The right token is being found.** Resolution order is flags, then environment
   (`AEROFLARE_GITHUB_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`), then the OS keychain. A
   stale `GITHUB_TOKEN` in your shell silently wins over the good one in your
   keychain.
3. **Package ownership.** The first push creates the package under the user or org
   that owns the token. If the package already exists and is owned by an org with
   restricted package creation, the push is refused no matter how well-scoped the
   token is.

In CI, pass the credential as a password, not a bearer token — the registry
performs the exchange itself. See [Authentication](./authentication.md).

## The build succeeds but nothing is pushed

`aeroflare run` learns which store paths to upload by reading the paths that the
wrapped command prints. If the command prints nothing, there is nothing to push,
and `run` exits successfully having done nothing.

Add `--print-out-paths`:

```bash
aeroflare run -- nix build .#default --print-out-paths
```

This is the single most common surprise with `run`. If a build seems to cache
nothing at all, check this before anything else.

## Nix downloads the NAR, then refuses the path

You will see something like `signature is not valid` or `cannot add path ... it
is not signed by a trusted key`.

Nix fetched your artifact and then rejected it, because your cache's public key
is not in its trust list. The cache is working; the client does not trust it.

Add the key to `nix.conf` (`/etc/nix/nix.conf`, or `~/.config/nix/nix.conf`):

```
trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= my-cache:AbCd...=
```

Keep `cache.nixos.org-1` in the list — it is space-separated and replacing it
will break upstream substitution.

See [Signing Keys](./signing-keys.md) for how to produce and publish the key.

## Nix ignores the substituter entirely

The build runs from source as if Aeroflare were not there, and no request ever
reaches the proxy.

The Nix daemon discards `extra-substituters` supplied by a user who is not
trusted. Add yourself to `trusted-users` in `/etc/nix/nix.conf`:

```
trusted-users = root your-username
```

Then restart the daemon (`sudo systemctl restart nix-daemon`, or on macOS
`sudo launchctl kickstart -k system/org.nixos.nix-daemon`). Until the daemon is
restarted, the setting has no effect — which makes this look as if the fix did
not work.

## `nix run github:ItzEmoji/aeroflare` runs an old version

Flake references are cached. To force a re-resolve:

```bash
nix run github:ItzEmoji/aeroflare --refresh -- version
```

Or pin an explicit revision. Check what you are actually running with
`aeroflare version`.

## Seeing what the proxy is doing

`-v` logs at the package level; `-vv` logs every outgoing HTTP request to the
registry, which is what you want when diagnosing a lookup that returns nothing:

```bash
aeroflare proxy -vv
```

A `404` on a manifest fetch means the store hash is genuinely not in the cache —
the proxy will fall through to the upstream cache, which is working as intended.
