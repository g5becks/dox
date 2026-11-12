#!/usr/bin/env bash
# Check a committed ref in clean clones; never include working-tree changes.
set -euo pipefail

# Parse the whole runner before executing jobs, so later workspace edits cannot
# change the commands read by an already-running matrix.
main() {

if [[ "$(uname -s)" != Darwin ]]; then
  echo "ci-local requires macOS for its native macOS legs" >&2
  exit 1
fi

repo=$(git rev-parse --show-toplevel)
ref=${1:-HEAD}
sha=$(git -C "$repo" rev-parse --verify "$ref^{commit}")
base=${DOX_RELEASE_ROOT:-/Users/takinprofit/Dev/dox-release}
work="$base/ci-local/$sha"
mkdir -p "$work/logs"
run_dir=$(mktemp -d "$work/run.XXXXXX")
logs="$work/logs/$(basename "$run_dir")"
mkdir -p "$logs"
results="$logs/results.txt"

if ! docker info >"$logs/docker-info.log" 2>&1; then
  for sock in "$HOME/.config/colima/default/docker.sock" "$HOME/.colima/default/docker.sock"; do
    if [[ -S "$sock" ]]; then
      export DOCKER_HOST="unix://$sock"
      break
    fi
  done
fi
if ! docker info >>"$logs/docker-info.log" 2>&1; then
  printf 'Docker is not reachable; see %s\nRESULT: FAIL\n' "$logs/docker-info.log"
  exit 1
fi

# Resolve supported patch versions once so both native macOS legs are explicit.
curl --fail --silent --show-error --location 'https://go.dev/dl/?mode=json' >"$logs/go-releases.json"
versions=$(python3 - "$logs/go-releases.json" <<'PY'
import json, sys
releases = json.load(open(sys.argv[1]))
for line in ('1.26', '1.27'):
    matches = [r['version'] for r in releases if r.get('stable') and r['version'].startswith('go'+line+'.')]
    if not matches:
        raise SystemExit('No supported Go release found for '+line)
    print(matches[0])
PY
)
printf '%s\n' "$versions" >"$logs/go-versions.txt"
failed=0
rows=()
clone_ref() {
  git clone --quiet --no-hardlinks "$repo" "$1"
  git -C "$1" checkout --quiet --detach "$sha"
}
record() {
  rows+=("| $1 | $2 |")
  if [[ "$2" != PASS ]]; then failed=1; fi
}

while IFS= read -r version; do
  line=${version#go}
  line=${line%.*}
  host_dir="$run_dir/macos-$line"
  clone_ref "$host_dir"
  printf 'Running macOS checks (%s)\n' "$version"
  if (cd "$host_dir" && DOX_GO_TOOLCHAIN="$version" bash scripts/ci-check.sh) >"$logs/macos-$line.log" 2>&1; then
    record "macOS / $version" PASS
  else
    record "macOS / $version" FAIL
  fi
  printf 'Running Windows cross-check (%s)\n' "$version"
  if (cd "$host_dir" && DOX_GO_TOOLCHAIN="$version" bash scripts/ci-check.sh windows-cross) >"$logs/windows-$line.log" 2>&1; then
    record "Windows amd64 compile + vet / $version" PASS
  else
    record "Windows amd64 compile + vet / $version" FAIL
  fi
  linux_dir="$run_dir/linux-$line"
  clone_ref "$linux_dir"
  build_cache="$base/ci-local/cache/linux-$line"
  mkdir -p "$build_cache"
  printf 'Running Linux checks (golang:%s)\n' "$line"
  if docker run --rm \
      --user "$(id -u):$(id -g)" \
      -e GOENV=off -e GOPATH=/cache/gopath -e GOMODCACHE=/go/pkg/mod -e GOCACHE=/cache \
      -e GOLANGCI_LINT_CACHE=/cache/golangci-lint \
      -v "$linux_dir:/w" -v "$HOME/go/pkg/mod:/go/pkg/mod" \
      -v "$build_cache:/cache" -w /w "golang:$line" \
      bash -c 'DOX_GO_TOOLCHAIN=$(GOTOOLCHAIN=local go env GOVERSION) bash scripts/ci-check.sh' >"$logs/linux-$line.log" 2>&1; then
    record "Linux / golang:$line" PASS
  else
    record "Linux / golang:$line" FAIL
  fi
  printf 'Running HTTPS container smoke (%s)\n' "$version"
  if DOX_GO_TOOLCHAIN="$version" bash "$host_dir/scripts/container-smoke.sh" >"$logs/container-$line.log" 2>&1; then
    record "Container HTTPS / $version" PASS
  else
    record "Container HTTPS / $version" FAIL
  fi
done <<<"$versions"

{
  printf 'ci-local ref: %s\ncommit: %s\nlogs: %s\n\n' "$ref" "$sha" "$logs"
  printf '| Job | Result |\n| --- | --- |\n'
  printf '%s\n' "${rows[@]}"
  printf '\nWindows checks compile tests and run vet; native Windows execution is GitHub-only.\n'
  if [[ "$failed" == 0 ]]; then echo 'RESULT: PASS'; else echo 'RESULT: FAIL'; fi
} | tee "$results"
exit "$failed"

}

main "$@"
