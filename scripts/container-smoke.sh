#!/usr/bin/env bash
# Build locally and verify real HTTPS with the same image used by GoReleaser.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
# Colima shares the release root, while the macOS default TMPDIR is not shared.
if [[ "$(uname -s)" == Darwin ]]; then
  base=${DOX_RELEASE_ROOT:-/Users/takinprofit/Dev/dox-release}
  mkdir -p "$base"
  scratch=$(mktemp -d "$base/container-smoke.XXXXXX")
else
  scratch=$(mktemp -d)
fi
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/context" "$scratch/work"

architecture=$(docker version --format '{{.Server.Arch}}')
case "$architecture" in
  arm64|aarch64) architecture=arm64 ;;
  amd64|x86_64) architecture=amd64 ;;
  *) printf 'Unsupported Docker architecture: %s\n' "$architecture" >&2; exit 1 ;;
esac
platform="linux/$architecture"
mkdir -p "$scratch/context/$platform"
if [[ -n "${1:-}" ]]; then
  cp "$1" "$scratch/context/$platform/dox"
else
  toolchain=${DOX_GO_TOOLCHAIN:-$(go env GOVERSION)}
  CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" GOTOOLCHAIN="$toolchain" \
    go build -trimpath -ldflags='-s -w' -o "$scratch/context/$platform/dox" ./cmd/dox
fi
cp Dockerfile "$scratch/context/Dockerfile"
image="dox-smoke:$(git rev-parse --short HEAD)-$(basename "$scratch")"
docker build --platform "$platform" --build-arg "TARGETPLATFORM=$platform" -t "$image" "$scratch/context"
image_user=$(docker image inspect --format '{{.Config.User}}' "$image")
if [[ "$image_user" != '65532' && "$image_user" != '65532:65532' ]]; then
  printf 'Expected nonroot image user, got %s\n' "$image_user" >&2
  exit 1
fi
cat > "$scratch/work/dox.toml" <<'CONFIG'
output = ".dox"
[sources.hono]
type = "url"
url = "https://hono.dev/llms.txt"
filename = "llms.txt"
CONFIG
# Match the bind-mount owner's UID/GID, including on Colima and hosted Linux.
docker run --rm --platform "$platform" --user "$(id -u):$(id -g)" \
  -v "$scratch/work:/workspace" -w /workspace "$image" sync
if [[ ! -s "$scratch/work/.dox/hono/llms.txt" ]]; then
  echo 'HTTPS sync produced no documentation file' >&2
  exit 1
fi
printf 'Container HTTPS smoke: PASS (%s, default user %s, %s bytes)\n' \
  "$platform" "$image_user" "$(wc -c < "$scratch/work/.dox/hono/llms.txt" | tr -d ' ')"
