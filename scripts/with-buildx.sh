#!/usr/bin/env bash
# Use project-pinned Buildx without installing a global Docker CLI plugin.
set -euo pipefail

if [[ $# == 0 ]]; then
  echo 'Usage: scripts/with-buildx.sh <command> [args...]' >&2
  exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd)
real_docker=$(command -v docker)
buildx=$(go tool -modfile="$root/tools/buildx/go.mod" -n buildx)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cat >"$scratch/docker" <<'DISPATCH'
#!/bin/sh
if [ "${1:-}" = buildx ]; then
  shift
  exec "$DOX_BUILDX" "$@"
fi
exec "$DOX_DOCKER" "$@"
DISPATCH
chmod +x "$scratch/docker"
export DOX_DOCKER="$real_docker" DOX_BUILDX="$buildx"
# Buildx state is separate from Docker's existing configuration/credentials.
export BUILDX_CONFIG="$scratch/buildx"
PATH="$scratch:$PATH" "$@"
