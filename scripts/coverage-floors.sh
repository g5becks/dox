#!/usr/bin/env bash
# Enforce per-package statement coverage floors against a `go test -coverprofile`
# file. Floors live in scripts/coverage-floors.txt ("<package> <percent>", with
# packages relative to the module root).
#
# The check is strict in both directions so it cannot rot silently:
#   - a package below its floor fails;
#   - a package present in the profile without a floor fails, so new packages
#     must choose a floor (0 is allowed for untested helpers);
#   - a floor for a package absent from the profile fails, so renames and
#     deletions update the file.
#
# Usage: scripts/coverage-floors.sh <coverprofile> [floors-file]
# Run from the repository root; DOX_COVERAGE_MODULE overrides the module path.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo 'usage: coverage-floors.sh <coverprofile> [floors-file]' >&2
  exit 2
fi
profile=$1
floors=${2:-$(cd "$(dirname "$0")" && pwd)/coverage-floors.txt}
module=${DOX_COVERAGE_MODULE:-$(awk '$1 == "module" { print $2; exit }' go.mod)}

for file in "$profile" "$floors"; do
  if [[ ! -r "$file" ]]; then
    echo "coverage-floors: cannot read $file" >&2
    exit 2
  fi
done
if [[ -z "$module" ]]; then
  echo 'coverage-floors: cannot determine the module path' >&2
  exit 2
fi

# go tool cover -func reports only per-function and total percentages, so the
# per-package figure is computed from the profile the same way `go test` does:
# covered statements over all statements, counting each block once.
status=0
report=$(awk -v module="$module" -v floors="$floors" '
  FILENAME == floors {
    sub(/#.*/, "")
    if (NF == 0) next
    if (NF != 2 || $2 !~ /^[0-9]+(\.[0-9]+)?$/) {
      printf "coverage-floors: %s:%d: want \"<package> <percent>\", got: %s\n", floors, FNR, $0 > "/dev/stderr"
      malformed = 1
      next
    }
    if ($1 in floor) {
      printf "coverage-floors: %s:%d: duplicate floor for %s\n", floors, FNR, $1 > "/dev/stderr"
      malformed = 1
    }
    floor[$1] = $2 + 0
    next
  }
  /^mode:/ { next }
  NF != 3 {
    printf "coverage-floors: malformed profile line %d: %s\n", FNR, $0 > "/dev/stderr"
    malformed = 1
    next
  }
  {
    block = $1
    file = block
    sub(/:[^:]*$/, "", file)
    pkg = file
    sub(/\/[^\/]*$/, "", pkg)
    if (pkg == module) {
      pkg = "."
    } else if (index(pkg, module "/") == 1) {
      pkg = substr(pkg, length(module) + 2)
    }
    owner[block] = pkg
    stmts[block] = $2 + 0
    if ($3 + 0 > hits[block] + 0) hits[block] = $3 + 0
  }
  END {
    for (block in stmts) {
      total[owner[block]] += stmts[block]
      if (hits[block] > 0) covered[owner[block]] += stmts[block]
    }
    failed = malformed
    for (pkg in total) {
      pct = total[pkg] ? 100 * covered[pkg] / total[pkg] : 100
      if (!(pkg in floor)) {
        printf "%s\t%.2f\t-\tFAIL no floor registered\n", pkg, pct
        failed = 1
      } else if (pct < floor[pkg]) {
        printf "%s\t%.2f\t%s\tFAIL below floor\n", pkg, pct, floor[pkg]
        failed = 1
      } else {
        printf "%s\t%.2f\t%s\tok\n", pkg, pct, floor[pkg]
      }
    }
    for (pkg in floor) {
      if (!(pkg in total)) {
        printf "%s\t-\t%s\tFAIL floor for a package missing from the profile\n", pkg, floor[pkg]
        failed = 1
      }
    }
    exit failed
  }
' "$floors" "$profile") || status=$?

{
  printf 'package\tcoverage%%\tfloor%%\tresult\n'
  sort <<<"$report"
} | awk -F '\t' '{ printf "%-34s %9s %7s  %s\n", $1, $2, $3, $4 }'
if [[ $status -ne 0 ]]; then
  echo 'coverage-floors: FAIL (raise tests, or lower a floor in scripts/coverage-floors.txt with a reason)' >&2
  exit 1
fi
echo 'coverage-floors: PASS'
