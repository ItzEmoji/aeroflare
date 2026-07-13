#!/usr/bin/env bash
# Tiny assertion harness for the shell tests. Sourced, not executed.

ASSERT_PASS=0
ASSERT_FAIL=0

assert_eq() {
  local desc=$1 expected=$2 actual=$3
  if [ "$expected" = "$actual" ]; then
    ASSERT_PASS=$((ASSERT_PASS + 1))
    printf '  ok   %s\n' "$desc"
  else
    ASSERT_FAIL=$((ASSERT_FAIL + 1))
    printf '  FAIL %s\n       expected: %q\n       actual:   %q\n' "$desc" "$expected" "$actual"
  fi
}

assert_contains() {
  local desc=$1 needle=$2 haystack=$3
  case "$haystack" in
    *"$needle"*)
      ASSERT_PASS=$((ASSERT_PASS + 1))
      printf '  ok   %s\n' "$desc" ;;
    *)
      ASSERT_FAIL=$((ASSERT_FAIL + 1))
      printf '  FAIL %s\n       missing: %q\n       in:      %q\n' "$desc" "$needle" "$haystack" ;;
  esac
}

# assert_fails <desc> <cmd...> — asserts the command exits non-zero.
#
# The command MUST run in a subshell. `die` calls `exit 1`, and lib.sh is sourced
# into the test's own shell, so calling it directly would terminate the test run
# silently — which reads as a pass. The `( … )` contains the exit.
assert_fails() {
  local desc=$1
  shift
  if ( "$@" ) >/dev/null 2>&1; then
    ASSERT_FAIL=$((ASSERT_FAIL + 1))
    printf '  FAIL %s (expected non-zero exit)\n' "$desc"
  else
    ASSERT_PASS=$((ASSERT_PASS + 1))
    printf '  ok   %s\n' "$desc"
  fi
}

report() {
  printf '  -- %d passed, %d failed\n' "$ASSERT_PASS" "$ASSERT_FAIL"
  [ "$ASSERT_FAIL" -eq 0 ]
}
