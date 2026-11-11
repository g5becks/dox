# Reproducible benchmarks

`dox.toml` pins the audit's named doc sources to immutable commits. The fetched
corpus contains **3,661 files / 23,490,248 bytes (22.4 MiB)**. The audit reported
3,519 files; its individual commit pins were not supplied. These pinned current
snapshots preserve the named source mix and approximate byte size. Keep these
pins fixed when comparing performance. Counts, sizes, and the Bun hash are in
`corpus-summary.json`.

```sh
GITHUB_TOKEN=$(gh auth token) bash scripts/bench-corpus.sh
export DOX_BENCH_CORPUS="$HOME/.cache/dox-bench"
GOTOOLCHAIN=go1.27.4 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkManifestGenerate(Full|Noop)|BenchmarkOutline(LargeMarkdown|Pathological)|BenchmarkSearch(ContentLiteral|ContentRegex|Metadata)|BenchmarkGitHubDirectory)$' \
  -benchmem -benchtime=1x -count=3 \
  ./internal/manifest ./internal/parser ./internal/search ./internal/source
```

Use Go 1.27.4 explicitly until the default-toolchain security follow-up lands.
The corpus setup requires an authenticated token because the original downloader
uses one API request per file. It runs without `--clean`. The environment variable
names the directory containing `dox.toml`, `docs/`, and `corpus-summary.json`.
Corpus benchmarks skip with a message if the variable/input is missing; the
synthetic and fake-HTTP benchmarks work offline.

The Bun website generates `llms-full.txt` outside the `oven-sh/bun` Git tree.
Its immutable input here is the [Bun 1.3.14 archived snapshot](https://github.com/reclear-io/llmref/tree/505462c6eb75db46b5012b06d2866d0f11d6e024/registry/bun/1.3.14),
whose metadata records `https://bun.sh`, snapshot date 2026-06-12, and MIT
redistribution permission. Its 2,070,979-byte content SHA-256 is
`3b87dca4793adbeebc2d382a55640021ef643aa74c975dc5aba9482da19043a8`, matching
the archive fingerprint. The setup script verifies it. Outline benchmarks parse
this `.txt` as Markdown deliberately, including 64/256/512 KiB prefixes.

Regenerate the exactly 1 MiB synthetic input with:

```sh
GOTOOLCHAIN=go1.27.4 go generate ./internal/parser
```

The generator emits geometrically increasing lines with long space runs. The old
Markdown table-header probe repeatedly rescans those runs despite the absence of
a table. The checked-in fixture demonstrates superlinear parsing without a large
third-party file or network access. Timing growth is measured in benchmarks,
never asserted in a flaky unit test.

`internal/testutil/githubfixture` serves an API, commit-pinned raw routes, and a
valid gzip tarball. `BenchmarkGitHubDirectory` syncs 35 and 108 files with 50 ms
latency per request, recording time, allocations, and REST quota separately from
raw/archive traffic. Set `DOX_BENCH_LATENCY=0s` for quick local exploration; use
the default 50 ms for comparisons. The fake server reports quota cost but does
not enforce the real unauthenticated 60-request limit. No credentials are used
by the loopback fixture.

`baseline-v0.3.0.txt` contains three one-iteration samples per benchmark, measured
against production code at tag v0.3.0 (`c316379`) in a temporary checkout. Only
these benchmark/helper files and the synthetic fixture were added there; the
production implementation and its dependencies were unchanged. The machine was
Apple M5, darwin/arm64, Go 1.27.4, `GOMAXPROCS=10`. One iteration keeps the
~39-second large-Markdown case practical. Three samples support descriptive
medians, not strong statistical confidence.

Compare matching runs with the pinned tool:

```sh
go tool -modfile=tools/go.mod benchstat testdata/bench/baseline-v0.3.0.txt current.txt
```

Full-manifest benchmarks copy inputs into a temporary directory and discard only
that temporary manifest between timed iterations. No-op benchmarks generate it
once before timing. Search uses the audit's literal `query`, regex `use[A-Z]\w+`,
and fuzzy metadata `routing`, with no result limit. Corpus copying and manifest
loading are excluded from the timed work.

## v0.3.0 medians

| Benchmark | Median time | Median B/op | REST/op |
| --- | ---: | ---: | ---: |
| ManifestGenerateFull | 710.796 ms | 277,359,040 | 0 |
| ManifestGenerateNoop | 705.744 ms | 277,380,800 | 0 |
| OutlinePathological/256KiB | 31.925 ms | 773,504 | 0 |
| OutlinePathological/1024KiB | 443.144 ms | 3,091,904 | 0 |
| OutlineLargeMarkdown/64KiB | 1.902 ms | 475,456 | 0 |
| OutlineLargeMarkdown/256KiB | 20.801 ms | 1,739,512 | 0 |
| OutlineLargeMarkdown/512KiB | 205.571 ms | 3,917,216 | 0 |
| OutlineLargeMarkdown/2022KiB | 39.000 s | 17,679,824 | 0 |
| SearchContentLiteral | 113.001 ms | 98,405,088 | 0 |
| SearchContentRegex | 284.093 ms | 65,169,528 | 0 |
| SearchMetadata | 13.023 ms | 32,129,688 | 0 |
| GitHubDirectory/35files | 1.950 s | 726,848 | 37 |
| GitHubDirectory/108files | 5.769 s | 2,084,960 | 110 |

Pathological growth (median 1 MiB / 256 KiB): **13.88×**.
