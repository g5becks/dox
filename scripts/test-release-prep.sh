#!/usr/bin/env bash
# Exercise release preflights in owned local repos; no network or publication.
set -euo pipefail
# Fixtures use only local origins and must not inherit signing/hooks/credentials.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
counter=0
setup() {
  counter=$((counter + 1))
  fixture="$scratch/$counter"
  mkdir -p "$fixture/repo/scripts" "$fixture/bin"
  cp "$root/scripts/release-prep.sh" "$fixture/repo/scripts/"
  git -C "$fixture/repo" init -q -b release/v0.4.0
  git -C "$fixture/repo" config user.name 'Release test'
  git -C "$fixture/repo" config user.email 'release-test@example.invalid'
  git -C "$fixture/repo" config core.attributesfile /dev/null
  git -C "$fixture/repo" config core.excludesfile /dev/null
  cat >"$fixture/repo/CHANGELOG.md" <<'CHANGELOG'
# Changelog

## [Unreleased]

### Changed

- A verified change.

## [0.3.0] - 2026-02-09

- Prior release.
CHANGELOG
  git -C "$fixture/repo" add -A
  git -C "$fixture/repo" commit -qm 'test: seed release fixture'
  git clone -q --bare --no-hardlinks "$fixture/repo" "$fixture/origin.git"
  git -C "$fixture/repo" remote add origin "$fixture/origin.git"
  for executable in go docker python3 curl; do
    cat >"$fixture/bin/$executable" <<'STUB'
#!/bin/sh
: >"$RELEASE_TEST_CALLED"
echo 'Dry-run invoked a build/network dependency' >&2
exit 91
STUB
    chmod +x "$fixture/bin/$executable"
  done
}
run_case() {
  expected=$1; shift
  before=$(git -C "$fixture/repo" show-ref)
  changelog=missing
  if [[ -f $fixture/repo/CHANGELOG.md ]]; then changelog=$(git -C "$fixture/repo" hash-object CHANGELOG.md); fi
  status=0
  output=$(PATH="$fixture/bin:$PATH" RELEASE_TEST_CALLED="$fixture/called" \
    DOX_RELEASE_ROOT="$fixture/artifacts" "$fixture/repo/scripts/release-prep.sh" "$@" 2>&1) || status=$?
  if [[ $expected == success ]]; then
    [[ $status == 0 && $output == *'No files, commits, tags, artifacts or remote refs changed.'* ]] || { printf '%s\n' "$output"; exit 1; }
    [[ ! -e $fixture/artifacts ]] || { echo 'dry-run created artifacts'; exit 1; }
  else
    [[ $status != 0 && $output == *"$expected"* ]] || { printf 'Expected %s; exit %s: %s\n' "$expected" "$status" "$output"; exit 1; }
  fi
  [[ $before == "$(git -C "$fixture/repo" show-ref)" ]] || { echo 'preflight changed refs'; exit 1; }
  after_changelog=missing
  if [[ -f $fixture/repo/CHANGELOG.md ]]; then after_changelog=$(git -C "$fixture/repo" hash-object CHANGELOG.md); fi
  [[ $changelog == "$after_changelog" ]] || { echo 'preflight changed changelog'; exit 1; }
  [[ ! -e $fixture/called ]] || { echo 'preflight ran build/network dependencies'; exit 1; }
}
setup; run_case success 0.4.0 --dry-run
setup; run_case 'Usage:'
setup; run_case 'Usage:' 0.4.0 --unknown
setup; run_case 'version must' v0.4.0 --dry-run
setup; run_case 'version must' 01.4.0 --dry-run
setup; run_case 'version must' '0.4.0;false' --dry-run
setup; git -C "$fixture/repo" switch -qc another; run_case 'expected branch' 0.4.0 --dry-run
setup; git -C "$fixture/repo" checkout -q --detach; run_case 'release branch' 0.4.0 --dry-run
setup; echo dirty >>"$fixture/repo/CHANGELOG.md"; run_case 'must be clean' 0.4.0 --dry-run
setup; echo untracked >"$fixture/repo/untracked"; run_case 'must be clean' 0.4.0 --dry-run
setup; echo staged >"$fixture/repo/staged"; git -C "$fixture/repo" add staged; run_case 'must be clean' 0.4.0 --dry-run
setup
printf '# Changelog\n\n## [Unreleased]\n\n### Changed\n\n## [0.3.0]\n\n- Prior entry.\n' >"$fixture/repo/CHANGELOG.md"
git -C "$fixture/repo" add CHANGELOG.md; git -C "$fixture/repo" commit -qm 'test: empty release section'
run_case 'has no entries' 0.4.0 --dry-run
setup
rm "$fixture/repo/CHANGELOG.md"
git -C "$fixture/repo" add CHANGELOG.md; git -C "$fixture/repo" commit -qm 'test: missing changelog'
run_case 'CHANGELOG.md is missing' 0.4.0 --dry-run
setup; git -C "$fixture/repo" tag v0.4.0; run_case 'already exists locally' 0.4.0 --dry-run
# Create a tag directly in the owned bare fixture; never push a tag anywhere.
setup; git -C "$fixture/origin.git" tag v0.4.0; run_case 'already exists on origin' 0.4.0 --dry-run
setup
mkdir -p "$fixture/artifacts/v0.4.0"
run_case 'artifact directory already exists' 0.4.0 --dry-run
printf 'Release-prep preflight checks: PASS (%s cases; dry-run/ref immutability; no publication)\n' "$counter"
