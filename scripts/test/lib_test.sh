#!/usr/bin/env bash
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=scripts/test/assert.sh
source "$HERE/assert.sh"
# shellcheck source=scripts/lib.sh
source "$HERE/../lib.sh"

echo "lib_test.sh"

assert_eq "arch_label X64"   "x86_64"  "$(arch_label X64)"
assert_eq "arch_label ARM64" "aarch64" "$(arch_label ARM64)"
assert_fails "arch_label rejects unknown" arch_label RISCV

assert_eq "token_env_var ghcr.io"        "AEROFLARE_TOKEN_GHCR_IO"        "$(token_env_var ghcr.io)"
assert_eq "token_env_var docker.io"      "AEROFLARE_TOKEN_DOCKER_IO"      "$(token_env_var docker.io)"
assert_eq "token_env_var localhost:5000" "AEROFLARE_TOKEN_LOCALHOST_5000" "$(token_env_var localhost:5000)"

assert_eq "read_version reads version.json" "$(jq -r '.["."]' "$HERE/../../version.json")" \
  "$(read_version "$HERE/../..")"
assert_fails "read_version dies on missing file" read_version /nonexistent-dir

assert_eq "split_list newlines" $'a\nb' "$(split_list $'a\nb')"
assert_eq "split_list commas"   $'a\nb' "$(split_list 'a,b')"
assert_eq "split_list trims"    $'a\nb' "$(split_list ' a , b ')"
assert_eq "split_list drops empties" $'a\nb' "$(split_list $'a\n\n,b,')"
assert_eq "split_list empty input" "" "$(split_list '')"

assert_eq "is_true true"  "yes" "$(is_true true  x && echo yes || echo no)"
assert_eq "is_true TRUE"  "yes" "$(is_true TRUE  x && echo yes || echo no)"
assert_eq "is_true 1"     "yes" "$(is_true 1     x && echo yes || echo no)"
assert_eq "is_true false" "no"  "$(is_true false x && echo yes || echo no)"
assert_eq "is_true empty" "no"  "$(is_true ''    x && echo yes || echo no)"
assert_fails "is_true rejects garbage" is_true maybe skip-attestation

assert_eq "normalize_tag bare"     "v1.2.3" "$(normalize_tag 1.2.3)"
assert_eq "normalize_tag v-prefix" "v1.2.3" "$(normalize_tag v1.2.3)"
assert_fails "normalize_tag rejects empty" normalize_tag ''

assert_eq "validate_repo accepts owner/repo" "yes" \
  "$(validate_repo ItzEmoji/aeroflare release-repo && echo yes || echo no)"
assert_eq "validate_repo accepts dots and dashes" "yes" \
  "$(validate_repo me/aero.flare-fork release-repo && echo yes || echo no)"
assert_fails "validate_repo rejects a bare name" validate_repo aeroflare release-repo
assert_fails "validate_repo rejects a ref suffix" validate_repo me/fork@v1 release-repo
assert_fails "validate_repo rejects a URL" validate_repo https://github.com/me/fork release-repo

# --- config_scalar ----------------------------------------------------------
CFG=$(mktemp)
cat > "$CFG" <<'EOF'
builds:
  - .#default
release-repo: me/aeroflare-fork
release-version: "v1.2.3"
skip-attestation: true   # trust me
quoted-single: 'a/b'
empty-key:
nested:
  inner-key: not-a-top-level-value
EOF

assert_eq "config_scalar reads a plain value" "me/aeroflare-fork" \
  "$(config_scalar "$CFG" release-repo)"
assert_eq "config_scalar strips double quotes" "v1.2.3" \
  "$(config_scalar "$CFG" release-version)"
assert_eq "config_scalar strips single quotes" "a/b" \
  "$(config_scalar "$CFG" quoted-single)"
assert_eq "config_scalar strips an inline comment" "true" \
  "$(config_scalar "$CFG" skip-attestation)"
assert_eq "config_scalar on a valueless key" "" "$(config_scalar "$CFG" empty-key)"
assert_eq "config_scalar ignores absent keys" "" "$(config_scalar "$CFG" nope)"
assert_eq "config_scalar ignores indented keys" "" "$(config_scalar "$CFG" inner-key)"
assert_eq "config_scalar on a missing file" "" "$(config_scalar /nonexistent release-repo)"
rm -f "$CFG"

report
