#!/usr/bin/env bash
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$HERE/../.." && pwd)
# shellcheck source=scripts/test/assert.sh
source "$HERE/assert.sh"

echo "run_test.sh"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# Stub binary: prints its argv one entry per line, then the token env vars it saw.
cat > "$WORK/aeroflare-ci" <<'EOF'
#!/usr/bin/env bash
for a in "$@"; do printf 'ARG:%s\n' "$a"; done
env | grep '^AEROFLARE_TOKEN_' | sort | sed 's/^/ENV:/' || true
EOF
chmod +x "$WORK/aeroflare-ci"

# run_sh <env assignments...> — invoke run.sh with a clean INPUT_* slate.
run_sh() {
  env -i PATH="$PATH" HOME="$HOME" \
      AEROFLARE_CI_BIN="$WORK/aeroflare-ci" \
      "$@" bash "$REPO_ROOT/scripts/run.sh" 2>&1
}

# --- configless mode --------------------------------------------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS=$'.#default\n.#foo')
assert_contains "configless passes --cache"  "ARG:--cache"           "$out"
assert_contains "configless cache value"     "ARG:ghcr.io;me/cache"  "$out"
assert_contains "configless first build"     "ARG:.#default"         "$out"
assert_contains "configless second build"    "ARG:.#foo"             "$out"
assert_eq       "configless emits 2 --build" "2" "$(grep -c '^ARG:--build$' <<<"$out")"
assert_eq       "no stray --config"          "0" "$(grep -c '^ARG:--config$' <<<"$out")"

# --- empty scalars produce no flags ----------------------------------------
assert_eq "no --workers when unset"     "0" "$(grep -c '^ARG:--workers$' <<<"$out")"
assert_eq "no --compression when unset" "0" "$(grep -c '^ARG:--compression$' <<<"$out")"

# --- scalars ---------------------------------------------------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='.#default' \
             INPUT_COMPRESSION=xz INPUT_WORKERS=8 \
             INPUT_UPSTREAM_CACHE=none INPUT_SIGNING_KEY=NIX_KEY)
assert_contains "passes --compression" "ARG:--compression" "$out"
assert_contains "compression value"    "ARG:xz"            "$out"
assert_contains "passes --workers"     "ARG:--workers"     "$out"
assert_contains "workers value"        "ARG:8"             "$out"
assert_contains "passes --upstream-cache" "ARG:--upstream-cache" "$out"
assert_contains "passes --signing-key"    "ARG:--signing-key"    "$out"
assert_eq "single upstream-cache emits exactly 1 --upstream-cache" \
  "1" "$(grep -c '^ARG:--upstream-cache$' <<<"$out")"
assert_contains "upstream-cache none value" "ARG:none" "$out"

# --- upstream-cache: newline- and comma-separated lists ---------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='.#default' \
             INPUT_UPSTREAM_CACHE=$'https://a\nhttps://b')
assert_eq "newline-separated upstream-cache emits 2 flags" \
  "2" "$(grep -c '^ARG:--upstream-cache$' <<<"$out")"
assert_contains "newline-separated first entry"  "ARG:https://a" "$out"
assert_contains "newline-separated second entry" "ARG:https://b" "$out"

out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='.#default' \
             INPUT_UPSTREAM_CACHE='https://a,https://b')
assert_eq "comma-separated upstream-cache emits 2 flags" \
  "2" "$(grep -c '^ARG:--upstream-cache$' <<<"$out")"
assert_contains "comma-separated first entry"  "ARG:https://a" "$out"
assert_contains "comma-separated second entry" "ARG:https://b" "$out"

# --- upstream-cache: unset produces no flag ---------------------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='.#default')
assert_eq "no --upstream-cache when unset" \
  "0" "$(grep -c '^ARG:--upstream-cache$' <<<"$out")"

# --- config mode ------------------------------------------------------------
out=$(run_sh INPUT_CONFIG=.aeroflare-ci.yaml)
assert_contains "config passes --config" "ARG:--config"           "$out"
assert_contains "config value"           "ARG:.aeroflare-ci.yaml" "$out"
assert_eq "config mode adds no --build" "0" "$(grep -c '^ARG:--build$' <<<"$out")"
assert_eq "config mode adds no --cache" "0" "$(grep -c '^ARG:--cache$' <<<"$out")"

# --- cache-token ------------------------------------------------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='.#default' INPUT_CACHE_TOKEN=sekrit)
assert_contains "exports AEROFLARE_TOKEN_GHCR_IO" "ENV:AEROFLARE_TOKEN_GHCR_IO=sekrit" "$out"

out=$(run_sh INPUT_CACHE='localhost:5000;me/cache' INPUT_BUILDS='.#default' INPUT_CACHE_TOKEN=sekrit)
assert_contains "host with port" "ENV:AEROFLARE_TOKEN_LOCALHOST_5000=sekrit" "$out"

# --- validation -------------------------------------------------------------
fails() { run_sh "$@" >/dev/null 2>&1; }

assert_fails "config + builds is an error" \
  fails INPUT_CONFIG=c.yaml INPUT_BUILDS='.#default'
assert_fails "config + cache is an error" \
  fails INPUT_CONFIG=c.yaml INPUT_CACHE='ghcr.io;me/cache'
assert_fails "neither mode is an error" fails
assert_fails "cache without builds is an error" \
  fails INPUT_CACHE='ghcr.io;me/cache'
assert_fails "builds without cache is an error" \
  fails INPUT_BUILDS='.#default'
assert_fails "two caches is an error" \
  fails INPUT_CACHE=$'ghcr.io;a\ndocker.io;b' INPUT_BUILDS='.#default'

out=$(run_sh INPUT_CONFIG=c.yaml INPUT_BUILDS='.#default' 2>&1 || true)
assert_contains "mutual-exclusion message is actionable" "mutually exclusive" "$out"


# --- base / on-missing-base -------------------------------------------------
out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='changed' INPUT_BASE='origin/main')
assert_contains "base passes --base"      "ARG:--base"     "$out"
assert_contains "base value"              "ARG:origin/main" "$out"

out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='changed' INPUT_ON_MISSING_BASE='error')
assert_contains "on-missing-base flag"  "ARG:--on-missing-base" "$out"
assert_contains "on-missing-base value" "ARG:error"             "$out"

out=$(run_sh INPUT_CACHE='ghcr.io;me/cache' INPUT_BUILDS='changed')
assert_eq "no --base when unset" \
  "0" "$(grep -c '^ARG:--base$' <<<"$out")"
assert_eq "no --on-missing-base when unset" \
  "0" "$(grep -c '^ARG:--on-missing-base$' <<<"$out")"
report
