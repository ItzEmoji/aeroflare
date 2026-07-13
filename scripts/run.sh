#!/usr/bin/env bash
# Validate the action's mode, then exec aeroflare-ci with the matching argv.
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

bin=${AEROFLARE_CI_BIN:?AEROFLARE_CI_BIN is required}

builds=${INPUT_BUILDS:-}
cache=${INPUT_CACHE:-}
config=${INPUT_CONFIG:-}

# --- mode validation --------------------------------------------------------
# ci.Resolve makes an inline list REPLACE the config file's list, so accepting
# both would silently discard the file's builds/caches. Refuse instead.
if [ -n "$config" ] && { [ -n "$builds" ] || [ -n "$cache" ]; }; then
  die "'config' and 'builds'/'cache' are mutually exclusive: an inline list replaces the config file's list. Use one mode or the other."
fi
if [ -z "$config" ] && { [ -z "$builds" ] || [ -z "$cache" ]; }; then
  die "set either 'config', or both 'cache' and 'builds'"
fi

args=()

if [ -n "$config" ]; then
  args+=(--config "$config")
else
  mapfile -t cache_entries < <(split_list "$cache")
  if [ "${#cache_entries[@]}" -ne 1 ]; then
    die "'cache' takes exactly one <registry>;<repository> entry (got ${#cache_entries[@]}); use 'config' for multiple caches"
  fi
  args+=(--cache "${cache_entries[0]}")

  mapfile -t build_entries < <(split_list "$builds")
  for b in "${build_entries[@]}"; do
    args+=(--build "$b")
  done

  # The push token for this one registry, named the way ResolveToken expects.
  if [ -n "${INPUT_CACHE_TOKEN:-}" ]; then
    registry=${cache_entries[0]%%;*}
    registry=${registry#http://}
    registry=${registry#https://}
    export "$(token_env_var "$registry")"="$INPUT_CACHE_TOKEN"
  fi
fi

# upstream-cache is optional and, like builds, may be a newline- or
# comma-separated list: emit one --upstream-cache per entry.
if [ -n "${INPUT_UPSTREAM_CACHE:-}" ]; then
  mapfile -t upstream_cache_entries < <(split_list "$INPUT_UPSTREAM_CACHE")
  for u in "${upstream_cache_entries[@]}"; do
    args+=(--upstream-cache "$u")
  done
fi

# --- scalars: append only when set, so the binary's defaults still apply -----
# Full `if` blocks, not `[ -n "$x" ] && args+=(...)`: the latter is safe under
# `set -e` (a failing test is exempt as a non-final member of an && list) but it
# trips shellcheck SC2015 and misleads readers.
if [ -n "${INPUT_COMPRESSION:-}" ];    then args+=(--compression    "$INPUT_COMPRESSION");    fi
if [ -n "${INPUT_WORKERS:-}" ];        then args+=(--workers        "$INPUT_WORKERS");        fi
if [ -n "${INPUT_SIGNING_KEY:-}" ];    then args+=(--signing-key    "$INPUT_SIGNING_KEY");    fi

exec "$bin" "${args[@]}"
