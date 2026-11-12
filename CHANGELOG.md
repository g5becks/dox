# Changelog

All notable changes are recorded here following Keep a Changelog. Versions follow
Semantic Versioning. Historical entries are reconstructed from the tagged commits.

## [Unreleased]

### Changed

- CLI errors now print actionable `hint:` lines to `stderr`, include `fields:` and `cause:` chains with `--verbose`, emit `{"error":{"code":"…","message":"…","hint":"…","fields":{…}}}` to `stderr` when `--json` is passed on any command, and exit with distinct status codes (`0` ok, `1` runtime/network, `2` usage/config, `3` partial sync failure, `4` not found, `130` interrupted). Scripts that previously checked specifically for exit status `1` on any failure should check for non-zero exit status or match the specific code (`2`, `3`, `4`, `130`), and JSON consumers should parse error envelopes from `stderr`.

- Release packaging uses current GoReleaser archive, cask and Docker configuration. Homebrew retains macOS universal and Linux amd64/arm64 coverage through a cask: run `brew update`, remove an existing formula with `brew uninstall --formula dox`, then `brew install --cask g5becks/tap/dox`. Existing archive/package names and multi-platform plus architecture-specific image tags remain supported. Custom Docker build contexts must place the binary at `linux/<arch>/dox`; pass `--build-arg TARGETPLATFORM=linux/<arch>` with a legacy builder.

- Manifest generation reuses unchanged entries, parses changes in parallel, honors cancellation, and avoids rewriting unchanged snapshots. JSON is now compact with additive parser/SHA cache metadata. `generated` derives from input timestamps; without a lock timestamp, `last_sync` uses file mtime. Consumers should parse JSON and use source/lock timestamps for freshness rather than treat generation time or file rewrites as a clock. Existing manifests regenerate automatically.

- Markdown/MDX outlines now use a linear line scanner with original source positions, matched code fences, and YAML/TOML metadata. `gomarkdown` is removed; Goldmark is limited to 256 KiB of inline text per file and metadata decoding to 64 KiB. Re-run `dox sync` after upgrading to regenerate corrected heading positions and metadata; oversized inline text keeps normalized source and oversized metadata is ignored. Corpus differences and fixture corrections are documented in `internal/parser/outline/README.md`.

- The Docker image now uses a digest-pinned Distroless base with CA certificates and default nonroot UID 65532. HTTPS syncs work inside the image. For project bind mounts, migrate root-dependent setups to `--user "$(id -u):$(id -g)"` or grant UID 65532 access to the mounted directory.

- `cat --line N` and `file:N` now accept the same 1-based locations reported by search and outlines; JSON adds a 1-based `line` while retaining `offset`. Migrate `--offset N` to `--line N+1`: the old 0-based flag warns and is scheduled for removal in v0.6.0. Conflicting and nonpositive locations are rejected.

- Sync parallelism now uses flag > config > computed default. An omitted `--parallel` honors `max_parallel`; the computed fallback is `min(max(4 × CPU cores, 10), 16)`. Pass `--parallel 3` to retain the previous accidental default limit. Explicit positive flag/config values are not capped at 16.

- Output writes and cleanup are confined to opened directory roots. Migrate `output = "."`, `".."`, or absolute paths to a dedicated relative directory such as `.dox`; intentional external directories require `dox --allow-external-output <command>`. Source `out` and `filename` paths must remain local. `clean` and `sync --clean` now require `.dox.lock` or `.dox-root`; run a normal `dox sync` first to mark existing output. Whole-output cleanup keeps the directory and ownership sentinel. Config directories, their ancestors, and filesystem roots remain protected even with the opt-in.

- Content search scans bytes with bounded parallel file reads and reusable buffers, preserving result order, line text, Unicode literal matching, and limit behavior while reducing allocations.

- Replaced the error library with typed internal errors and standard wrapping while preserving messages, hints, context, and legacy code identifiers.

- Refreshed CLI, config, validation, globbing, table, and fuzzy-search dependencies. Command help is unchanged; updated x/net, x/text, and OpenTelemetry remove their reported dependency advisories.

- Building from source requires Go 1.26 or newer; development uses Go 1.27.4. Upgrade older Go installations before building. Go 1.26 retains compatibility with the oldest supported Go line while enabling the standard library modernizations.

- Applied Go modernizers and simplified sorted map iteration and test contexts without changing CLI behavior.
- Git hooks now format staged Go files and check new lint findings before commits; full lint and race tests run before pushes. Install with `go tool -modfile=tools/go.mod lefthook install`.
- Moved development tools into `tools/go.mod` and pinned golangci-lint. Run Task with `go tool -modfile=tools/go.mod task` to use the repository's pinned version. Beads v0.49.6 is isolated in `tools/beads/go.mod`; continue using `scripts/bd`.

### Added

- `dox version` subcommand that prints the same version, commit, and build-time metadata as `dox --version`.

- Local release preparation validates preconditions, creates a local annotated tag, runs the complete CI/snapshot/smoke checks, records binary sizes, and prints maintainer publication commands without executing them.

- Project-pinned Buildx and a temporary Docker dispatcher make local snapshot image builds reproducible without a global Docker plugin installation.

- Audit regression cases with an explicit known-failure registry; unexpected successes require removing the corresponding entries before release.

- End-to-end CLI testscript coverage and reusable fake GitHub fault-injection and JSON-golden helpers.

- A commit-pinned documentation corpus, reproducible v0.3.0 performance baselines, a generated pathological Markdown fixture, and latency/counting GitHub API/raw/archive benchmark fixtures. See `testdata/bench/README.md`.

- A shared CI quality gate, a three-OS/two-Go GitHub matrix, and `scripts/ci-local.sh` for clean-ref macOS/Linux checks plus Windows vet/test compilation while GitHub Actions is unavailable. Release publication requires the CI gate.

### Fixed

- GitHub rate-limit classification now inspects 403/429 responses before status handling, waits when the reset is within 60 seconds, otherwise fails with `*errs.RateLimitError` and the `GITHUB_TOKEN`/`GH_TOKEN` hint, succeeds with a single low-quota warning when a 2xx response has `X-RateLimit-Remaining <= 10` (including `0`), and retries only 5xx/network errors (up to 3 retries) with jittered backoff, honoring `Retry-After` up to the same 60-second bound; 4xx responses are never retried. Warnings appear on stderr as `⚠ <source>: …` and are omitted from `--json` output.

- `dox sync` now enforces configurable HTTP and per-source deadlines (`timeout = "30s"` in `dox.toml`, overridden by `dox sync --timeout <duration>`), isolates stalled sources so sibling sources still complete, honors `SIGINT`/`SIGTERM` cancellation across sync/manifest/search while saving completed sources' lock entries before exiting `130`.

- Default builds and development tools use Go 1.27.4, addressing reachable standard-library advisory GO-2026-6859. The Go 1.26 minimum remains unchanged.

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
