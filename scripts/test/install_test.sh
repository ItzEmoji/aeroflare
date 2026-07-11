#!/usr/bin/env bash
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$HERE/../.." && pwd)
# shellcheck source=scripts/test/assert.sh
source "$HERE/assert.sh"

echo "install_test.sh"

# The real PATH, kept so each setup() can find mktemp/ln before it installs the
# hermetic PATH the tests actually run under.
ORIG_PATH=$PATH

# Everything install.sh (and the gh stub) may exec. The PATH under test contains
# these and nothing else, so `rm $WORK/bin/nix` genuinely removes nix — with the
# real PATH still appended, /run/current-system/sw/bin/nix would satisfy
# `command -v nix` and the preflight test would pass vacuously.
TOOLS=(bash cat chmod cp dirname env grep jq ln mkdir rm sed tar tr zstd)

setup() {
  PATH=$ORIG_PATH
  WORK=$(mktemp -d)
  mkdir -p "$WORK/bin" "$WORK/temp" "$WORK/assets"

  local t
  for t in "${TOOLS[@]}"; do
    ln -s "$(command -v "$t")" "$WORK/bin/$t"
  done

  # A stub "aeroflare-ci" binary, packaged exactly like the release asset:
  # binary at bin/aeroflare-ci inside the archive.
  mkdir -p "$WORK/assets/bin"
  printf '#!/bin/sh\necho stub-ci "$@"\n' > "$WORK/assets/bin/aeroflare-ci"
  chmod +x "$WORK/assets/bin/aeroflare-ci"
  tar --zstd -cf "$WORK/assets/aeroflare-ci-x86_64.tar.zst" -C "$WORK/assets" bin/aeroflare-ci

  cat > "$WORK/bin/nix" <<'EOF'
#!/bin/sh
exit 0
EOF

  # gh stub: `release download` copies the prebuilt archive into the --dir the
  # real install.sh passes. `attestation verify` succeeds. Both honour the
  # FAKE_GH_* flags to simulate failures. $FAKE_ASSETS locates the archive.
  cat > "$WORK/bin/gh" <<'EOF'
#!/bin/sh
case "$1 $2" in
  "release download")
    [ "${FAKE_GH_DOWNLOAD_FAIL:-0}" = 1 ] && exit 1
    dest=
    while [ $# -gt 0 ]; do
      case "$1" in
        --dir|-D) dest=$2; shift ;;
      esac
      shift
    done
    [ -n "$dest" ] || exit 1
    cp "$FAKE_ASSETS/aeroflare-ci-x86_64.tar.zst" "$dest/" || exit 1
    exit 0 ;;
  "attestation verify")
    [ "${FAKE_GH_VERIFY_FAIL:-0}" = 1 ] && exit 1
    exit 0 ;;
esac
exit 0
EOF
  chmod +x "$WORK/bin/nix" "$WORK/bin/gh"

  GITHUB_OUTPUT="$WORK/gh_output"; : > "$GITHUB_OUTPUT"
  export GITHUB_OUTPUT
  export FAKE_ASSETS="$WORK/assets"
  export RUNNER_TEMP="$WORK/temp"
  export GITHUB_ACTION_PATH="$REPO_ROOT"
  export GH_TOKEN=fake
  export RUNNER_OS=Linux RUNNER_ARCH=X64
  # Exported by individual cases; clear them so no case leaks into the next.
  unset FAKE_GH_DOWNLOAD_FAIL FAKE_GH_VERIFY_FAIL
  export PATH="$WORK/bin"
}
teardown() { PATH=$ORIG_PATH; rm -rf "$WORK"; }

run_install() { bash "$REPO_ROOT/scripts/install.sh" 2>&1; }

# --- happy path -------------------------------------------------------------
setup
out=$(run_install); rc=$?
assert_eq "install.sh succeeds" "0" "$rc"
assert_contains "emits bin= output" "bin=$RUNNER_TEMP/aeroflare/bin/aeroflare-ci" "$(cat "$GITHUB_OUTPUT")"
assert_eq "extracted binary is executable" "yes" \
  "$([ -x "$RUNNER_TEMP/aeroflare/bin/aeroflare-ci" ] && echo yes || echo no)"
teardown

# --- preflight --------------------------------------------------------------
setup; export RUNNER_OS=macOS
out=$(run_install); rc=$?
assert_eq "non-Linux exits 1" "1" "$rc"
assert_contains "non-Linux annotates" "::error::" "$out"
assert_contains "non-Linux explains" "Linux" "$out"
teardown

setup; rm "$WORK/bin/nix"
out=$(run_install); rc=$?
assert_eq "missing nix exits 1" "1" "$rc"
assert_contains "missing nix names an installer" "nix-installer-action" "$out"
teardown

setup; export RUNNER_ARCH=RISCV
out=$(run_install); rc=$?
assert_eq "bad arch exits 1" "1" "$rc"
assert_contains "bad arch annotates" "unsupported RUNNER_ARCH" "$out"
teardown

# --- failure modes ----------------------------------------------------------
setup; export FAKE_GH_DOWNLOAD_FAIL=1
out=$(run_install); rc=$?
assert_eq "missing asset exits 1" "1" "$rc"
assert_contains "missing asset is actionable" "pin the action to" "$out"
teardown

setup; export FAKE_GH_VERIFY_FAIL=1
out=$(run_install); rc=$?
assert_eq "failed attestation exits 1" "1" "$rc"
assert_contains "failed attestation annotates" "provenance" "$out"
teardown

report
