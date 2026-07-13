#!/usr/bin/env bash
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
rc=0
for t in "$HERE"/*_test.sh; do
  bash "$t" || rc=1
done
exit "$rc"
