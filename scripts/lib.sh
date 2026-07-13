#!/usr/bin/env bash
# Shared helpers for the aeroflare composite action. Sourced, not executed.
# Sourcing must have no side effects.

# die <message> — emit a GitHub error annotation and abort.
die() {
  printf '::error::%s\n' "$1" >&2
  exit 1
}

# arch_label <runner_arch> — map RUNNER_ARCH to the release asset's arch label.
arch_label() {
  case "$1" in
    X64)   printf 'x86_64\n' ;;
    ARM64) printf 'aarch64\n' ;;
    *)     die "unsupported RUNNER_ARCH '$1'; aeroflare ships linux x86_64 and aarch64 only" ;;
  esac
}

# token_env_var <registry> — mirrors TokenEnvVar in internal/ci/cachespec.go.
# ghcr.io -> AEROFLARE_TOKEN_GHCR_IO
token_env_var() {
  local host=$1
  host=${host//./_}
  host=${host//:/_}
  printf 'AEROFLARE_TOKEN_%s\n' "$(printf '%s' "$host" | tr '[:lower:]' '[:upper:]')"
}

# read_version <action_path> — the version release-please maintains, e.g. 1.8.0.
read_version() {
  local manifest=$1/version.json version
  [ -f "$manifest" ] || die "no version.json at $manifest"
  version=$(jq -er '.["."]' "$manifest") || die "version.json has no \".\" key"
  printf '%s\n' "$version"
}

# split_list <string> — split on newlines and commas, trim, drop empties,
# emit one entry per line. Mirrors splitEnvList in cmd/aeroflare-ci/main.go.
split_list() {
  printf '%s' "$1" \
    | tr ',' '\n' \
    | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | grep -v '^$' || true
}
