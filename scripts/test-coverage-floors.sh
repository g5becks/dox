#!/usr/bin/env bash
# Regression checks for scripts/coverage-floors.sh.
set -euo pipefail

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
script="$(cd "$(dirname "$0")" && pwd)/coverage-floors.sh"
export DOX_COVERAGE_MODULE=example.com/m

# a: 3 of 4 statements covered (75%); b: fully covered; blocks repeat with
# different counts, which must be merged by taking the larger one.
cat > "$scratch/profile" <<'PROFILE'
mode: atomic
example.com/m/a/a.go:1.1,2.2 2 1
example.com/m/a/a.go:3.1,4.2 1 0
example.com/m/a/a.go:3.1,4.2 1 0
example.com/m/a/a.go:5.1,6.2 1 7
example.com/m/b/b.go:1.1,2.2 3 1
example.com/m/b/b.go:1.1,2.2 3 0
example.com/m/main.go:1.1,2.2 1 1
PROFILE

run() { # <floors> -> sets output, returns the script's status
  printf '%s\n' "$1" > "$scratch/floors"
  bash "$script" "$scratch/profile" "$scratch/floors" > "$scratch/output" 2>&1
}
accept() {
  if ! run "$1"; then
    printf 'Floors rejected but should pass:\n%s\n' "$1" >&2
    cat "$scratch/output" >&2
    exit 1
  fi
}
reject() { # <floors> <expected message>
  if run "$1"; then
    printf 'Floors accepted but should fail (%s):\n%s\n' "$2" "$1" >&2
    exit 1
  fi
  if ! grep -q -- "$2" "$scratch/output"; then
    printf 'Missing %q in output:\n' "$2" >&2
    cat "$scratch/output" >&2
    exit 1
  fi
}

accept $'a 75\nb 100\n. 100'
accept $'# comment\na 74.99 # trailing comment\n\nb 0\n. 0'
reject $'a 75.01\nb 100\n. 100' 'below floor'
reject $'a 75\n. 100' 'no floor registered'
reject $'a 75\nb 100\nc 0\n. 100' 'missing from the profile'
reject $'a 75\nb 100\n. 100\n. 100' 'duplicate floor'
reject $'a seventy\nb 100\n. 100' 'want "<package> <percent>"'

# Missing profile is a usage error, not a pass.
if bash "$script" "$scratch/absent" "$scratch/floors" > "$scratch/output" 2>&1; then
  echo 'Missing profile accepted' >&2
  exit 1
fi
echo 'coverage-floors tests: PASS'
