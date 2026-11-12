#!/usr/bin/env bash
# Shared checks for native CI jobs and ci-local clean clones.
set -euo pipefail

DOX_GO_TOOLCHAIN="${DOX_GO_TOOLCHAIN:-$(go env GOVERSION)}"
export DOX_GO_TOOLCHAIN
project_go() { GOTOOLCHAIN="$DOX_GO_TOOLCHAIN" go "$@"; }
tool() {
  local executable
  # Build the pinned tool with its required Go, then inspect the matrix Go.
  executable=$(GOTOOLCHAIN=go1.27.4 go tool -modfile=tools/go.mod -n "$1")
  shift
  GOTOOLCHAIN="$DOX_GO_TOOLCHAIN" "$executable" "$@"
}

project_go version
bash scripts/test-commit-msg.sh
if [[ "${1:-}" == windows-cross ]]; then
  export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
  project_go vet ./...
  scratch=$(mktemp -d)
  trap 'rm -rf "$scratch"' EXIT
  packages=$(project_go list ./...)
  while IFS= read -r package; do
    project_go test -c -o "$scratch/${package//\//_}.exe" "$package"
  done <<<"$packages"
  echo 'Windows vet and test compilation: PASS (native execution requires GitHub)'
  exit 0
fi

GOTOOLCHAIN=go1.27.4 go tool -modfile=tools/actionlint/go.mod actionlint
GOTOOLCHAIN=go1.27.4 go tool -modfile=tools/buildx/go.mod -n buildx >/dev/null
project_go build ./...
project_go vet ./...
CGO_ENABLED=0 tool golangci-lint run
project_go test -race -coverprofile=coverage.out ./...
tool govulncheck -show verbose ./...
if ! fix_diff=$(project_go fix -diff ./... 2>&1); then
  printf '%s\n' "$fix_diff"
  echo 'go fix failed or produced a diff' >&2
  exit 1
fi
if [[ -n "$fix_diff" ]]; then
  printf '%s\n' "$fix_diff"
  echo 'go fix produced a diff' >&2
  exit 1
fi
format_files=$(gofmt -l .)
if [[ -n "$format_files" ]]; then
  printf '%s\n' "$format_files"
  echo 'gofmt found unformatted files' >&2
  exit 1
fi
project_go mod tidy -diff
(cd tools && GOTOOLCHAIN=go1.27.4 go mod tidy -diff)
(cd tools/beads && GOTOOLCHAIN=go1.27.4 go mod tidy -diff)
(cd tools/actionlint && GOTOOLCHAIN=go1.27.4 go mod tidy -diff)
(cd tools/buildx && GOTOOLCHAIN=go1.27.4 go mod tidy -diff)
echo 'Native quality checks: PASS'
