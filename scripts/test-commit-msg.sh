#!/usr/bin/env bash
# Regression checks for the actual commit-msg hook; never bypass it.
set -euo pipefail

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
hook="$(cd "$(dirname "$0")" && pwd)/commit-msg-lint.sh"

accept() {
  printf '%s\n' "$1" > "$scratch/message"
  if ! "$hook" "$scratch/message" > "$scratch/output" 2>&1; then
    printf 'Valid subject rejected: %s\n' "$1" >&2
    cat "$scratch/output" >&2
    exit 1
  fi
}
reject() {
  printf '%s\n' "$1" > "$scratch/message"
  if "$hook" "$scratch/message" > "$scratch/output" 2>&1; then
    printf 'Invalid subject accepted: %s\n' "$1" >&2
    exit 1
  fi
}

accept 'fix: handle empty input'
accept 'docs(cli): document flags'
accept 'feat!: change defaults'
accept 'fix(paths)!: require contained output'
accept 'refactor(CLI_v2-core)!: simplify dispatch'
accept "Merge branch 'feature'"
accept 'fixup! fix: handle empty input'
accept 'squash! fix: handle empty input'
reject ''
reject 'unknown: invalid type'
reject 'fix missing colon'
reject 'fix(): empty scope'
reject 'fix(bad scope): invalid scope'
reject '!fix: misplaced marker'
reject 'fix!!: repeated marker'
reject 'fix(paths)!!: repeated marker'
reject 'fix(paths!): misplaced marker'
reject 'fix!: '
reject 'fix(paths)!: '
reject 'fix: '
reject 'fix:no space'
if "$hook" "$scratch/missing" > "$scratch/output" 2>&1; then
  echo 'Missing message file accepted' >&2
  exit 1
fi
printf 'Commit-message smoke checks: PASS\n'
