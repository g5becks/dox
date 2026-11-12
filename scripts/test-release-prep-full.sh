#!/usr/bin/env bash
# Expensive integration test: a committed release branch, local bare origin,
# actual ci-local/snapshot/smokes, and guards against publication. Keep evidence.
set -euo pipefail
version=${1:-0.4.0}
[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || exit 2
root=$(cd "$(dirname "$0")/.." && pwd)
base=${DOX_RELEASE_ROOT:-/Users/takinprofit/Dev/dox-release}
mkdir -p "$base"
fixture=$(mktemp -d "$base/release-prep-test.XXXXXX")
printf 'Integration fixture: %s\n' "$fixture"
real_git=$(command -v git)
real_go=$(command -v go)
real_docker=$(command -v docker)
sha=$(git -C "$root" rev-parse HEAD)
root_tags=$(git -C "$root" show-ref --tags)
# Only fixture repositories use this isolated identity/configuration.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git clone -q --bare --no-hardlinks "$root" "$fixture/origin.git"
git clone -q --branch "release/v$version" "$fixture/origin.git" "$fixture/repo"
[[ $(git -C "$fixture/repo" rev-parse HEAD) == "$sha" ]]
git -C "$fixture/repo" config user.name 'Release integration test'
git -C "$fixture/repo" config user.email 'release-test@example.invalid'
origin_refs=$(git --git-dir="$fixture/origin.git" show-ref)
mkdir "$fixture/bin"
cat >"$fixture/bin/git" <<'GUARD'
#!/bin/bash
for argument in "$@"; do
  if [[ $argument == push ]]; then echo 'Forbidden git push in release test' >&2; exit 97; fi
done
exec "$RELEASE_TEST_GIT" "$@"
GUARD
cat >"$fixture/bin/go" <<'GUARD'
#!/bin/bash
goreleaser=false; release=false; snapshot=false
for argument in "$@"; do
  case "$argument" in goreleaser) goreleaser=true ;; release) release=true ;; --snapshot) snapshot=true ;; esac
done
if $goreleaser && $release; then
  $snapshot || { echo 'Forbidden non-snapshot release' >&2; exit 98; }
  printf '%s\n' "$*" >>"$RELEASE_TEST_TRACE"
fi
exec "$RELEASE_TEST_GO" "$@"
GUARD
cat >"$fixture/bin/docker" <<'GUARD'
#!/bin/bash
for argument in "$@"; do
  case "$argument" in push|--push) echo 'Forbidden image publication' >&2; exit 99 ;; esac
done
exec "$RELEASE_TEST_DOCKER" "$@"
GUARD
chmod +x "$fixture/bin/"*
export RELEASE_TEST_GIT="$real_git" RELEASE_TEST_GO="$real_go" RELEASE_TEST_DOCKER="$real_docker"
export RELEASE_TEST_TRACE="$fixture/snapshot-commands.log"
export PATH="$fixture/bin:$PATH" DOX_RELEASE_ROOT="$fixture/releases"
cd "$fixture/repo"
go tool -modfile=tools/go.mod lefthook install >"$fixture/hooks.log" 2>&1
before=$(git show-ref)
bash scripts/release-prep.sh "$version" --dry-run >"$fixture/dry-run.log" 2>&1
[[ $before == "$(git show-ref)" && -z $(git status --porcelain) && ! -e $DOX_RELEASE_ROOT ]]
# Reuse only the approved build caches; every checkout/ref remains isolated.
if [[ -d $base/ci-local/cache ]]; then
  mkdir -p "$DOX_RELEASE_ROOT/ci-local"
  ln -s "$base/ci-local/cache" "$DOX_RELEASE_ROOT/ci-local/cache"
fi
bash scripts/release-prep.sh "$version" 2>&1 | tee "$fixture/preparation.log"
[[ $(git cat-file -t "v$version") == tag ]]
[[ $(git rev-parse HEAD^) == "$sha" ]]
[[ $(git log -1 --format=%s) == "chore(release): prepare v$version" ]]
[[ $(git diff --name-only "$sha" HEAD) == CHANGELOG.md ]]
[[ -z $(git status --porcelain) ]]
[[ $origin_refs == "$(git --git-dir="$fixture/origin.git" show-ref)" ]]
[[ $root_tags == "$(git -C "$root" show-ref --tags)" ]]
[[ -s $RELEASE_TEST_TRACE ]]
grep -qx 'RESULT: PASS (local preparation only; nothing published)' "$fixture/preparation.log"
printf 'Release-prep integration: PASS; annotated tag only in %s/repo; source tags and bare origin unchanged.\n' "$fixture"
