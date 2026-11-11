# Changelog

All notable changes are recorded here following Keep a Changelog. Versions follow
Semantic Versioning. Historical entries are reconstructed from the tagged commits.

## [Unreleased]

### Changed

- Output writes and cleanup are confined to opened directory roots. Migrate `output = "."`, `".."`, or absolute paths to a dedicated relative directory such as `.dox`; intentional external directories require `dox --allow-external-output <command>`. Source `out` and `filename` paths must remain local. `clean` and `sync --clean` now require `.dox.lock` or `.dox-root`; run a normal `dox sync` first to mark existing output. Whole-output cleanup keeps the directory and ownership sentinel. Config directories, their ancestors, and filesystem roots remain protected even with the opt-in.

- Content search scans bytes with bounded parallel file reads and reusable buffers, preserving result order, line text, Unicode literal matching, and limit behavior while reducing allocations.

- Replaced the error library with typed internal errors and standard wrapping while preserving messages, hints, context, and legacy code identifiers.

- Refreshed CLI, config, validation, globbing, table, and fuzzy-search dependencies. Command help is unchanged; updated x/net, x/text, and OpenTelemetry remove their reported dependency advisories.

- Building from source requires Go 1.26 or newer; development uses Go 1.27.3. Upgrade older Go installations before building. Go 1.26 retains compatibility with the oldest supported Go line while enabling the standard library modernizations.

- Applied Go modernizers and simplified sorted map iteration and test contexts without changing CLI behavior.
- Git hooks now format staged Go files and check new lint findings before commits; full lint and race tests run before pushes. Install with `go tool -modfile=tools/go.mod lefthook install`.
- Moved development tools into `tools/go.mod` and pinned golangci-lint. Run Task with `go tool -modfile=tools/go.mod task` to use the repository's pinned version. Beads v0.49.6 is isolated in `tools/beads/go.mod`; continue using `scripts/bd`.

### Added

- Audit regression cases with an explicit known-failure registry; unexpected successes require removing the corresponding entries before release.

- End-to-end CLI testscript coverage and reusable fake GitHub fault-injection and JSON-golden helpers.

- A commit-pinned documentation corpus, reproducible v0.3.0 performance baselines, a generated pathological Markdown fixture, and latency/counting GitHub API/raw/archive benchmark fixtures. See `testdata/bench/README.md`.

- A shared CI quality gate, a three-OS/two-Go GitHub matrix, and `scripts/ci-local.sh` for clean-ref macOS/Linux checks plus Windows vet/test compilation while GitHub Actions is unavailable. Release publication requires the CI gate.

### Fixed

- The binary ignore rule now applies only at the repository root, and commit-message hooks accept standard breaking-change subjects with `!`.

- `dox add` writes updated configuration through a private temporary file and rename, avoiding partial writes and changes through other hard links. Validation and sync orchestration were refactored to satisfy the pinned lint checks.

## [0.3.0] - 2026-02-09

### Added

- `dox search` with fuzzy metadata matches, full-text search, optional regular expressions, collection filters, result limits, and table/JSON/CSV output.
- Search benchmarks, including a 700-file index case.

### Fixed

- Filtered negative-score metadata matches and returned an empty JSON array for content searches without results.

## [0.2.0] - 2026-02-09

### Added

- Local manifests and `collections`, `files`, `cat`, and `outline` commands, with query defaults configured through `[display]`.
- Markdown, MDX, text, and TypeScript/TSX parsers for descriptions, headings, and exports, including documentation-component detection.

### Fixed

- Markdown and TSX heading/export line numbers, multi-line MDX import/export stripping, and cat pagination/newline/path handling.
- Manifest collection locations, lock timestamps, and empty-source handling.

## [0.1.0] - 2026-02-07

### Added

- TOML configuration discovery, validation, defaults, and source-type inference.
- GitHub Trees API and direct URL downloads, conditional requests, a local lockfile, concurrent sync, progress, and source error summaries.
- `sync`, `list`, `clean`, `init`, and `add` commands, including dry-run summaries and configurable parallelism.
- Initial release packaging and Homebrew support.

[Unreleased]: https://github.com/g5becks/dox/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/g5becks/dox/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/g5becks/dox/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/g5becks/dox/releases/tag/v0.1.0
