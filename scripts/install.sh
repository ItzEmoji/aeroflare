#!/usr/bin/env bash
# Download, provenance-verify, and extract the aeroflare-ci release binary that
# matches this action's ref. Writes `bin=<path>` to $GITHUB_OUTPUT.
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

repo=${AEROFLARE_REPO:-ItzEmoji/aeroflare}

# --- preflight --------------------------------------------------------------
[ "${RUNNER_OS:-}" = "Linux" ] \
  || die "aeroflare supports Linux runners only (RUNNER_OS=${RUNNER_OS:-unset})"

command -v nix >/dev/null 2>&1 \
  || die "nix not found on PATH; add an installer step before this action, e.g. DeterminateSystems/nix-installer-action@v20"

# --- resolve ----------------------------------------------------------------
# $GITHUB_ACTION_PATH holds this repo at the ref the consumer pinned, so
# version.json names the exact release to download — for @v1, @main, or a SHA.
version=$(read_version "$GITHUB_ACTION_PATH")
arch=$(arch_label "${RUNNER_ARCH:-}")
tag="v$version"
archive="aeroflare-ci-$arch.tar.zst"
dest="$RUNNER_TEMP/aeroflare"
mkdir -p "$dest"

# --- download ---------------------------------------------------------------
gh release download "$tag" --repo "$repo" --pattern "$archive" --dir "$dest" \
  || die "release $tag of $repo ships no $archive; pin the action to a release that publishes assets (>= v1.8.0)"

# --- verify -----------------------------------------------------------------
gh attestation verify "$dest/$archive" --repo "$repo" \
  || die "provenance verification failed for $archive from $tag"

# --- extract ----------------------------------------------------------------
tar --zstd -xf "$dest/$archive" -C "$dest"
chmod +x "$dest/bin/aeroflare-ci"

printf 'bin=%s\n' "$dest/bin/aeroflare-ci" >> "$GITHUB_OUTPUT"
printf 'aeroflare-ci %s (%s) verified and installed\n' "$version" "$arch"
