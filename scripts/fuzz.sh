#!/usr/bin/env bash
# Run every fuzz target for a fixed time each.
#
#   scripts/fuzz.sh [fuzztime]   fuzztime defaults to 10s (CI smoke run)
#   task fuzz [FUZZTIME=5m]      long local run, defaults to 5m per target
#
# `go test -fuzz` accepts one target per invocation, so each target below gets
# its own run. Seed corpora also run as ordinary tests in `go test ./...`.
# Run from the repository root.
set -euo pipefail

fuzztime=${1:-${FUZZTIME:-10s}}
export GOTOOLCHAIN=${DOX_GO_TOOLCHAIN:-$(go env GOVERSION)}

# "<package directory> <fuzz function>"
targets=(
  './cmd/dox FuzzReplaceSourceSection'
  './internal/config FuzzLoadConfig'
  './internal/config FuzzValidatePath'
  './internal/parser FuzzOutlineScanner'
  './internal/source FuzzNormalizeRepoPath'
  './internal/source FuzzShouldIncludeFile'
)

# Refuse to skip a fuzz function that is not registered above.
found=$(grep -rn --include='*_test.go' -E '^func Fuzz[A-Za-z0-9_]*\(' . |
  sed -E 's#^(\./[^:]*)/[^/:]*_test\.go:[0-9]+:func (Fuzz[A-Za-z0-9_]*)\(.*#\1 \2#' | sort)
registered=$(printf '%s\n' "${targets[@]}" | sort)
if [[ "$found" != "$registered" ]]; then
  echo 'fuzz.sh: registered targets differ from Fuzz functions in the tree:' >&2
  diff <(printf '%s\n' "$registered") <(printf '%s\n' "$found") >&2 || true
  exit 1
fi

for target in "${targets[@]}"; do
  read -r package function <<<"$target"
  echo "== fuzz $package $function ($fuzztime)"
  go test -run '^$' -fuzz "^${function}\$" -fuzztime "$fuzztime" "$package"
done
echo "fuzz: PASS (${#targets[@]} targets, $fuzztime each)"
