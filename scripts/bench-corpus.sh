#!/usr/bin/env bash
# Sync immutable documentation inputs without deleting an existing corpus.
set -euo pipefail

main() {
repo=$(git rev-parse --show-toplevel)
corpus=${DOX_BENCH_CORPUS:-$HOME/.cache/dox-bench}
if [[ -z "${GITHUB_TOKEN:-${GH_TOKEN:-}}" ]]; then
  echo 'Set GITHUB_TOKEN to your GitHub token before syncing this API-heavy corpus.' >&2
  exit 1
fi
mkdir -p "$corpus"
corpus=$(cd "$corpus" && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cp "$repo/testdata/bench/dox.toml" "$corpus/dox.toml"
(cd "$repo" && go build -o "$scratch/dox" ./cmd/dox)
(cd "$corpus" && "$scratch/dox" sync --config "$corpus/dox.toml")
python3 - "$corpus" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
manifest = json.loads((root / 'docs/manifest.json').read_text())
collections = manifest['collections']
summary = {
    'files': sum(c['file_count'] for c in collections.values()),
    'bytes': sum(c['total_size'] for c in collections.values()),
    'collections': {name: {'files': c['file_count'], 'bytes': c['total_size']}
                    for name, c in sorted(collections.items())},
    'bun_sha256': hashlib.sha256((root / 'docs/bun/llms-full.txt').read_bytes()).hexdigest(),
}
if summary['bun_sha256'] != '3b87dca4793adbeebc2d382a55640021ef643aa74c975dc5aba9482da19043a8':
    raise SystemExit('Pinned Bun snapshot checksum mismatch')
(root / 'corpus-summary.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary, indent=2))
PY
printf 'Corpus ready: DOX_BENCH_CORPUS=%s\n' "$corpus"

}

main "$@"
