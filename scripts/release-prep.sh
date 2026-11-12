#!/usr/bin/env bash
# Prepare and verify a release locally. Publication belongs to the maintainer.
set -euo pipefail

die() { printf 'release-prep: %s\n' "$*" >&2; exit 1; }
usage() { echo 'Usage: scripts/release-prep.sh <version> [--dry-run]' >&2; exit 2; }
[[ $# -ge 1 && $# -le 2 ]] || usage
version=$1
dry_run=false
if [[ $# == 2 ]]; then [[ $2 == --dry-run ]] || usage; dry_run=true; fi
[[ $version =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || die 'version must be X.Y.Z without a v prefix'
tag="v$version"
repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
branch=$(git symbolic-ref --quiet --short HEAD) || die 'HEAD must be on a release branch'
[[ $branch == "release/$tag" ]] || die "expected branch release/$tag, got $branch"
[[ -z $(git --no-optional-locks status --porcelain=v1 --untracked-files=all) ]] || die 'working tree and index must be clean'
[[ -f CHANGELOG.md ]] || die 'CHANGELOG.md is missing'
entries=$(awk '
  /^## \[Unreleased\]$/ { inside=1; next }
  inside && /^## / { exit }
  inside && !/^#/ && !/^[[:space:]]*$/ { print }
' CHANGELOG.md)
[[ -n $entries ]] || die '[Unreleased] has no entries'
if git show-ref --verify --quiet "refs/tags/$tag"; then die "$tag already exists locally"; fi
export GIT_TERMINAL_PROMPT=0
remote_tags=$(git ls-remote --tags origin "refs/tags/$tag" "refs/tags/$tag^{}") || die 'cannot read origin tags; preflight made no changes'
[[ -z $remote_tags ]] || die "$tag already exists on origin"
base=${DOX_RELEASE_ROOT:-/Users/takinprofit/Dev/dox-release}
[[ $base == /* ]] || die 'DOX_RELEASE_ROOT must be absolute'
destination="$base/$tag"
[[ ! -e $destination ]] || die "artifact directory already exists: $destination"
if $dry_run; then
  printf 'Dry run: roll [Unreleased] to [%s], commit, annotate local %s, run ci-local, snapshot and smoke tests in %s.\n' "$version" "$tag" "$destination"
  echo 'No files, commits, tags, artifacts or remote refs changed.'
  exit 0
fi
[[ $(uname -s) == Darwin ]] || die 'real release preparation requires macOS for ci-local native jobs'
for executable in git go docker python3 curl; do command -v "$executable" >/dev/null || die "required executable missing: $executable"; done
if [[ -z ${DOCKER_HOST:-} && -S "$HOME/.config/colima/default/docker.sock" ]]; then
  export DOCKER_HOST="unix://$HOME/.config/colima/default/docker.sock"
fi
docker info >/dev/null || die 'Docker must already be running'
mkdir -p "$base"
mkdir "$destination"
scratch=$(mktemp -d "$destination/work.XXXXXX")
# Preserve evidence, commit and local tag on failure; never delete/repoint refs.
trap 'status=$?; if [[ $status != 0 ]]; then printf "release-prep failed; preserve local refs and inspect %s\n" "$destination" >&2; fi' EXIT
release_date=$(date -u +%F)
awk -v version="$version" -v date="$release_date" '
  /^## \[Unreleased\]$/ { print; printf "\n## [%s] - %s\n", version, date; next }
  { print }
' CHANGELOG.md >"$scratch/CHANGELOG.md"
cp "$scratch/CHANGELOG.md" CHANGELOG.md
git add CHANGELOG.md
git commit -m "chore(release): prepare $tag"
git tag -a "$tag" -m "dox $tag"
git rev-parse "$tag^{commit}" >"$destination/tag-commit.txt"
bash scripts/ci-local.sh "$tag" 2>&1 | tee "$destination/ci-tag.log"
grep -qx 'RESULT: PASS' "$destination/ci-tag.log" || die 'ci-local did not report PASS'
go tool -modfile=tools/go.mod goreleaser check 2>&1 | tee "$destination/goreleaser-check.log"
python3 - "$scratch/goreleaser.yaml" "$destination/artifacts" <<'PY'
from pathlib import Path
import sys
source = Path('.goreleaser.yaml').read_text()
needle = 'project_name: dox\n'
if source.count(needle) != 1:
    raise SystemExit('Expected one project_name for snapshot dist override')
# JSON quotes are valid YAML strings and preserve spaces/special characters.
import json
Path(sys.argv[1]).write_text(source.replace(needle, needle+'dist: '+json.dumps(sys.argv[2])+'\n', 1))
PY
scripts/with-buildx.sh go tool -modfile=tools/go.mod goreleaser release --snapshot --clean \
  --config "$scratch/goreleaser.yaml" 2>&1 | tee "$destination/snapshot.log"
python3 scripts/release-summary.py "$destination/artifacts" >"$destination/binaries.tsv"
darwin=$(awk -F '\t' '$1 == "darwin/arm64" { print $2 }' "$destination/binaries.tsv")
[[ -n $darwin ]] || die 'snapshot omitted darwin/arm64'
"$darwin" --version 2>&1 | tee "$destination/darwin-version.log"
mkdir "$scratch/mac-sync"
cat >"$scratch/mac-sync/dox.toml" <<'CONFIG'
output = ".dox"
[sources.hono]
type = "url"
url = "https://hono.dev/llms.txt"
filename = "llms.txt"
CONFIG
(cd "$scratch/mac-sync" && "$darwin" sync) 2>&1 | tee "$destination/darwin-sync.log"
[[ -s $scratch/mac-sync/.dox/hono/llms.txt ]] || die 'native URL sync produced no documentation'
base_image=$(awk '$1 == "FROM" { print $2 }' Dockerfile)
[[ $base_image == *@sha256:* ]] || die 'smoke base image must be digest-pinned'
architecture=$(docker version --format '{{.Server.Arch}}')
case "$architecture" in arm64|aarch64) architecture=arm64 ;; amd64|x86_64) architecture=amd64 ;; *) die "unsupported Docker architecture: $architecture" ;; esac
# Run foreign binaries with user-mode QEMU inside an ordinary nonroot container;
# never install/change the VM's binfmt handlers or use a privileged container.
qemu_image=tonistiigi/binfmt@sha256:400a4873b838d1b89194d982c45e5fb3cda4593fbfd7e08a02e76b03b21166f0
run_linux_binary() {
  local platform=$1 binary=$2 emulator
  if [[ $platform == "linux/$architecture" ]]; then
    printf 'Linux smoke: %s, native pinned CA base\n' "$platform"
    docker run --rm --platform "$platform" -v "$binary:/usr/local/bin/dox:ro" \
      --entrypoint /usr/local/bin/dox "$base_image" --version
  else
    case "$platform" in
      linux/amd64) emulator=qemu-x86_64 ;;
      linux/arm64) emulator=qemu-aarch64 ;;
      linux/arm/v7) emulator=qemu-arm ;;
      *) die "unsupported smoke platform: $platform" ;;
    esac
    printf 'Linux smoke: %s via %s in %s (UID65532)\n' "$platform" "$emulator" "$qemu_image"
    docker run --rm --platform "linux/$architecture" --user 65532 -v "$binary:/dox:ro" \
      --entrypoint "/usr/bin/$emulator" "$qemu_image" /dox --version
  fi
}
while IFS=$'\t' read -r platform binary size; do
  case "$platform" in
    linux/*)
      label=${platform//\//-}
      run_linux_binary "$platform" "$binary" 2>&1 | tee "$destination/$label-version.log"
      ;;
  esac
done <"$destination/binaries.tsv"
native_linux=$(awk -F '\t' -v platform="linux/$architecture" '$1 == platform { print $2 }' "$destination/binaries.tsv")
[[ -n $native_linux ]] || die 'snapshot omitted the Docker host architecture'
bash scripts/container-smoke.sh "$native_linux" 2>&1 | tee "$destination/container-https.log"
cat "$destination/binaries.tsv"
printf '\nPrepared local %s at %s. Artifacts and smoke logs: %s\n' "$tag" "$(cat "$destination/tag-commit.txt")" "$destination"
echo 'Maintainer commands (printed only; review/merge the release PR before pushing the tag):'
printf 'git push -u origin release/%s\n' "$tag"
printf 'gh pr create --base main --head release/%s --title "release: %s"\n' "$tag" "$tag"
printf 'git push origin %s\n' "$tag"
printf '%s\n' 'GITHUB_TOKEN=$(gh auth token) scripts/with-buildx.sh go tool -modfile=tools/go.mod goreleaser release --clean'
printf 'go install github.com/g5becks/dox/cmd/dox@%s && dox version\n' "$tag"
echo 'RESULT: PASS (local preparation only; nothing published)'
