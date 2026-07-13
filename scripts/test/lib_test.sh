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

report
