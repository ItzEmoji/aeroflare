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

# is_true <value> <input_name> — boolean action inputs. Unset and empty are
# false, so an omitted input keeps the safe default. Anything that is neither
# truthy nor falsy dies rather than silently picking a branch: a typo like
# `skip-attestation: ture` must not quietly turn verification back on.
is_true() {
  local raw=$1 name=$2
  case "$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')" in
    true|1|yes|on)      return 0 ;;
    ''|false|0|no|off)  return 1 ;;
    *) die "'$name' must be true or false, got '$raw'" ;;
  esac
}

# normalize_tag <version> — release tag for a version written with or without
# the leading v, so `1.2.3` and `v1.2.3` resolve to the same release.
normalize_tag() {
  local version=${1#v}
  [ -n "$version" ] || die "release version must not be empty"
  printf 'v%s\n' "$version"
}

# validate_repo <repo> <input_name> — reject anything that is not owner/repo.
# `gh` fails opaquely on `me/fork@v1` or a full URL; catching it here says why.
validate_repo() {
  [[ $1 =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]] \
    || die "'$2' must be owner/repo, got '$1'"
}

# config_scalar <file> <key> — read one top-level scalar from a YAML file.
#
# Deliberately minimal, and NOT a YAML parser. The install step runs before
# aeroflare-ci has been downloaded, so the keys that decide what to download
# cannot be read by the binary that normally parses the config, and no YAML tool
# is guaranteed on the runner. Only top-level keys are matched (the ^ anchor
# skips nested ones), values may be quoted, and an inline comment is stripped.
# Anchors, block scalars and multi-document files are out of scope — the keys
# this reads are flat scalars by definition.
config_scalar() {
  local file=$1 key=$2 value
  [ -f "$file" ] || return 0
  # Quit at the first match, so a duplicate key later in the file cannot win.
  value=$(sed -n "/^${key}:/{s/^${key}:[[:space:]]*//p;q;}" "$file")
  value=${value%%$'\r'}
  value=$(printf '%s' "$value" | sed -e 's/[[:space:]]#.*$//' -e 's/[[:space:]]*$//')
  case "$value" in
    '"'*'"') value=${value#\"}; value=${value%\"} ;;
    "'"*"'") value=${value#\'}; value=${value%\'} ;;
  esac
  printf '%s\n' "$value"
}

# split_list <string> — split on newlines and commas, trim, drop empties,
# emit one entry per line. Mirrors splitEnvList in cmd/aeroflare-ci/main.go.
split_list() {
  printf '%s' "$1" \
    | tr ',' '\n' \
    | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | grep -v '^$' || true
}
