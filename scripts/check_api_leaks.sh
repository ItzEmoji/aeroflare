#!/usr/bin/env bash
# Enforces two invariants that the Go compiler cannot check for us.
#
#   1. No internal/ type appears in the public API of a pkg/ package.
#      A pkg/ package MAY import internal/ -- that restriction only applies
#      outside the module -- so a leak compiles fine here. But an external
#      caller cannot name the type, which makes the API unusable for the very
#      people it was promoted for. Only a doc-level check catches this.
#
#      Fatal for the library packages. For pkg/cmd/** it is reported as a
#      warning: those are cobra command constructors whose intended API is
#      NewCmdX, and pkg/cmd/auth/shared has a known pre-existing leak of
#      internal/auth and internal/secrets types. Promoting those packages is a
#      separate decision; until it is made, do not let the noise hide a real
#      regression in the engines.
#
#   2. The public engines do not depend on internal/ui. Presentation belongs in
#      the command layer; a library must not write to its caller's stdout.
#
# Without these gates, the first convenience function someone adds silently
# undoes the decoupling.
set -euo pipefail

# Packages whose public API is a contract with external importers.
LIBRARY_PKGS='pkg/(oci|push|proxy|prepare|cmdutil|iostreams)'

status=0

echo "==> checking for internal/ types in public signatures"
for pkg in $(go list ./pkg/...); do
  # Which internal packages does this one import? go doc renders their types by
  # short name (e.g. "*ui.Box"), never by import path, so grepping for the path
  # would never match -- we have to look for the short names.
  internal_imports=$(go list -f '{{range .Imports}}{{println .}}{{end}}' "$pkg" \
    | grep 'aeroflare/internal/' || true)
  [ -z "$internal_imports" ] && continue

  # go doc -all indents prose with spaces; declarations start with a keyword or
  # a tab (struct fields, interface methods). Restrict to those so a mention in
  # a doc comment is not mistaken for a leak.
  api=$(go doc -all "$pkg" 2>/dev/null | grep -E '^(func|type|var|const)|^	' || true)

  while read -r imp; do
    [ -z "$imp" ] && continue
    short=$(basename "$imp")
    hits=$(printf '%s\n' "$api" | grep -E "(^|[^[:alnum:]_])${short}\." || true)
    [ -z "$hits" ] && continue

    if printf '%s\n' "$pkg" | grep -qE "$LIBRARY_PKGS"; then
      echo "FAIL: $pkg exposes types from $imp in its public API:"
      status=1
    else
      echo "warning: $pkg exposes types from $imp (command package, not a library contract):"
    fi
    printf '%s\n' "$hits" | sed 's/^/    /'
    echo "    (an external caller cannot name these types)"
  done <<<"$internal_imports"
done

echo "==> checking that the public engines do not depend on internal/ui"
for pkg in oci push proxy prepare; do
  if go list -deps "./pkg/$pkg/..." 2>/dev/null | grep -q 'aeroflare/internal/ui'; then
    echo "FAIL: pkg/$pkg depends on internal/ui; presentation belongs in pkg/cmd"
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "OK: public API is clean"
fi
exit "$status"
