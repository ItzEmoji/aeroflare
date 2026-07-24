#!/usr/bin/env bash
# Download, provenance-verify, and extract the aeroflare-ci release binary that
# matches this action's ref. Writes `bin=<path>` to $GITHUB_OUTPUT.
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

readonly DEFAULT_REPO=ItzEmoji/aeroflare

# --- preflight --------------------------------------------------------------
[ "${RUNNER_OS:-}" = "Linux" ] \
  || die "aeroflare supports Linux runners only (RUNNER_OS=${RUNNER_OS:-unset})"

command -v nix >/dev/null 2>&1 \
  || die "nix not found on PATH; add an installer step before this action, e.g. DeterminateSystems/nix-installer-action@v20"

# --- resolve the release source ---------------------------------------------
# Precedence: action input, then the config file, then the AEROFLARE_REPO env
# escape hatch, then the default. The config file is read here rather than by
# aeroflare-ci because these keys decide which aeroflare-ci to download — the
# binary that parses the config does not exist yet. See config_scalar in lib.sh.
repo=${INPUT_RELEASE_REPO:-}
version=${INPUT_RELEASE_VERSION:-}
skip_attestation=${INPUT_SKIP_ATTESTATION:-}

config=${INPUT_CONFIG:-}
if [ -n "$config" ]; then
  [ -n "$repo" ]             || repo=$(config_scalar "$config" release-repo)
  [ -n "$version" ]          || version=$(config_scalar "$config" release-version)
  [ -n "$skip_attestation" ] || skip_attestation=$(config_scalar "$config" skip-attestation)
fi

repo=${repo:-${AEROFLARE_REPO:-$DEFAULT_REPO}}
validate_repo "$repo" release-repo

# An unset version means "whatever this action's ref pins". $GITHUB_ACTION_PATH
# holds this repo at the ref the consumer pinned, so version.json names the exact
# release to download — for @v1, @main, or a SHA. An explicit `latest` instead
# leaves the tag empty, and `gh release download` then picks the newest release,
# which is what a fork or test repo with its own numbering needs.
case "$(printf '%s' "$version" | tr '[:upper:]' '[:lower:]')" in
  '')     tag=$(normalize_tag "$(read_version "$GITHUB_ACTION_PATH")") ;;
  latest) tag= ;;
  *)      tag=$(normalize_tag "$version") ;;
esac

if [ -n "$tag" ]; then
  source_desc="release $tag of $repo"
else
  source_desc="the latest release of $repo"
fi

arch=$(arch_label "${RUNNER_ARCH:-}")
archive="aeroflare-ci-$arch.tar.zst"
dest="$RUNNER_TEMP/aeroflare"
mkdir -p "$dest"

# --- download ---------------------------------------------------------------
download=(release download)
if [ -n "$tag" ]; then download+=("$tag"); fi
download+=(--repo "$repo" --pattern "$archive" --dir "$dest")

gh "${download[@]}" \
  || die "$source_desc ships no $archive; pin the action to a release that publishes assets (>= v1.8.0), or point 'release-repo'/'release-version' at one that does"

# --- verify -----------------------------------------------------------------
if is_true "$skip_attestation" skip-attestation; then
  printf '::warning::provenance verification skipped for %s; the downloaded binary is unverified\n' "$repo"
  verified="unverified"
else
  gh attestation verify "$dest/$archive" --repo "$repo" \
    || die "provenance verification failed for $archive from $source_desc; if this repo publishes no attestations, set 'skip-attestation: true'"
  verified="verified"
fi

# --- extract ----------------------------------------------------------------
tar --zstd -xf "$dest/$archive" -C "$dest"
chmod +x "$dest/bin/aeroflare-ci"

printf 'bin=%s\n' "$dest/bin/aeroflare-ci" >> "$GITHUB_OUTPUT"
printf 'aeroflare-ci %s (%s) from %s, %s and installed\n' \
  "${tag:-latest}" "$arch" "$repo" "$verified"
