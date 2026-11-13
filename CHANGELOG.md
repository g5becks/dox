# Changelog

All notable changes are recorded here following Keep a Changelog. Versions follow
Semantic Versioning. Historical entries are reconstructed from the tagged commits.

## [Unreleased]

### Changed

- The `--config` (`-c`) flag is now declared once, on the root command, instead of on each command. It is still accepted before or after the subcommand, so `dox files docs -c dox.toml` keeps working, and `DOX_CONFIG` sets it from the environment (the flag wins). `init` now honors it and creates the file at that path. **Migration:** none for scripts; remove per-command `--config` workarounds if you like.

- `files --all` is deprecated: it prints a warning and behaves as `--limit 0`, and it is removed in v0.6.0. Every list-style command (`collections`, `files`, `cat`, `search`) now follows one `--limit N` convention: `0` means no limit and an omitted flag uses `display.default_limit`. The "more files" hint now says `use --limit 0 to show all`. **Migration:** replace `--all` with `--limit 0`.

- `display.format` in the config now applies to `collections`, `files`, `cat`, `outline` and `search` as the `dox init` template documents; before, it was parsed and ignored, so a config with `format = "json"` or `"csv"` changes their default output. A `--format` flag or `--json` wins over the config. `display.format` also accepts `jsonl`. **Migration:** set `format = "table"` (the default) to keep the previous output.

- Config files are decoded strictly. Value types must match (`repo = 5` now reports `invalid config at "sources.docs.repo" (line 4) … (a string)` instead of a misleading validation error). **Unknown keys are now errors** listing each key and line (for example a misspelled `max_paralel`), where before they were silently ignored. Syntax errors name their line. All of these exit with code 2. Koanf and its dependencies (copystructure, reflectwalk, mapstructure, fsnotify) are removed in favor of go-toml directly. **Migration:** run `dox list` once; fix or remove any key it reports as unknown, and quote string values.

- `dox search --content --regex` with a pattern that does not compile now exits **2** (usage, with the hint `Check regex syntax`) instead of 1, matching every other bad argument and the exit code the Go API and `dox mcp` already report. **Migration:** scripts that branched on exit 1 for an invalid pattern should branch on 2; exit 1 now means only a runtime failure.

- The `lines` value in `manifest.json`, `dox files`, `dox cat --json` and `dox.File.Lines` now counts lines the way `cat` addresses them: a file that ends with a newline is no longer counted one line too many (a file of 11 lines was reported as 12), and an empty file has 0 lines instead of 1. Files without a trailing newline are unchanged, and the Go API's `Content.TotalLines` and the `dox mcp` `cat` tool already reported this count, so all surfaces now agree. The manifest parser revision moves from 2 to 3, so the next `dox sync` regenerates existing manifests locally, without re-downloading anything; until then `files` and `cat --json` keep showing the old values. **Migration:** anything that compared `lines` to a stored value or used it as an upper bound plus one should expect values lower by one for files ending in a newline; `dox sync` once refreshes the manifest.

### Added

- `--format table|json|jsonl|csv` on `list`, `collections`, `files`, `cat`, `outline` and `search`; `--json` stays an alias for `--format json`. JSON Lines writes one object per item and CSV a header plus one row per item (`cat` emits `line,text`, `outline` emits `kind,line,level,type,text`). `dox doctor` takes the same flags.

- `dox doctor` checks the config, per-host token presence (never the value), the github.com API rate limit (skipped by `--offline`), output-directory safety, and consistency between the config, the lock and the files on disk. A `fail` check exits 1.

- Unknown collections and files in `files`, `cat`, `outline` and `search --collection` suggest close matches (`Did you mean "goreleaser"?`) in the `hint:` line, still exiting with code 4.

- Shell completion is available as `dox completion bash|zsh|fish|pwsh`.

- `dox version` and `--version` fall back to `debug.ReadBuildInfo()`, so a binary built with `go install github.com/g5becks/dox/cmd/dox@vX.Y.Z` reports its version, commit and build time instead of `dev`.

- `dox sync` and `dox clean` take an advisory lock on the output directory (`flock` on Unix, `LockFileEx` on Windows, file `.dox-run.lock`), so concurrent runs no longer race on `.dox.lock` and `manifest.json`. A second run waits up to `--lock-timeout` (default `30s`) and then exits 1 with a clear message. `sync --clean` now cleans only after taking the lock. Dry runs and `--check` stay lock-free.

- Gitea, Forgejo and Codeberg sources (`type = "gitea"`, alias `"codeberg"`; inferred for hosts containing codeberg, gitea or forgejo; default host `codeberg.org`). They are commit-pinned, find the folder's tree with non-recursive `git/trees` listings (2 calls for a top-level folder), and list changes with paginated recursive trees, then download raw files or one repository archive. Tokens come from `tokens.<host>` or `GITEA_TOKEN`. Only `type = "git"` and unknown hosts without a type remain unsupported.

- GitLab sources (gitlab.com and self-managed, API v4), with subgroup project paths (`repo = "group/subgroup/project"`). They are commit-pinned and use 2 API calls to detect an unchanged folder (commit plus parent listing). Changed folders get paginated listings, then raw files or a single folder archive. GitLab `RateLimit-*` headers and 429 responses are classified like GitHub's. Tokens come from `tokens.<host>` or `GITLAB_TOKEN` (sent as `PRIVATE-TOKEN`). `type = "gitlab"` now defaults its host to `gitlab.com`, and `dox add --host gitlab.com` writes a complete GitLab source.

- GitHub Enterprise Server sources: set `host` to the instance (with `type = "github"` when the host name lacks "github"). dox uses `https://<host>/api/v3` and fetches content and archives through the API. Per-host tokens come from `tokens = { "<host>" = "..." }` or `GH_ENTERPRISE_TOKEN`. A source with a host and no `type` now infers its type from the host, so a `gitlab.com` or unknown host is reported as not supported rather than treated as GitHub. Internally, GitHub sources run through a provider interface that GitLab and Gitea will implement.

- `dox sync --check` reports staleness without writing anything: exit 0 when every source is current, 3 when any would change; `--json` prints `{"stale": [...], "current": [...]}`. `dox sync --locked` reproduces the lock: GitHub sources fetch the locked commits, URL sources must match their recorded content digest, and config drift fails with exit code 2. Lock entries now record a `config_digest`, and URL entries record the content's `sha256` under `files`. Locks written before v0.5.0 need one plain `dox sync` before `--locked`.

- A public Go API, `github.com/g5becks/dox/dox`: `dox.Load` a project, then `Sync`, `Collections`, `Files`, `Outline`, `Cat` and `Search` (an `iter.Seq2` of results) with the CLI's behavior. Every method returns a `*dox.Error` with a stable `Code` and a `Hint`, and wraps sentinels (`ErrCollectionNotFound`, `ErrFileNotFound`, `ErrSourceNotFound`, `ErrNotSynced`) for `errors.Is`. A `Project` is safe for concurrent use. The package may change before v1.0.0.

- `dox mcp` serves the synced documentation to AI agents over the Model Context Protocol (stdio) through five read-only tools: `collections`, `files`, `outline`, `cat` and `search`. Output is capped and paged (`cat` 500 lines by default, `search` 20 results, `files` 200 entries) and failures carry the CLI's hints. It is built on the official `github.com/modelcontextprotocol/go-sdk` v1.8.0, which adds six small modules (`jsonschema-go`, `segmentio/asm`, `segmentio/encoding`, `yosida95/uritemplate`, `x/oauth2`, `x/time`) and grows the stripped binary from 11.9 MB to 13.9 MB. Requests piped to the server are all answered before it exits at the end of input.

- CI enforces per-package coverage floors (`scripts/coverage-floors.txt`; `cmd/dox` 70, `internal/sync` 80, `internal/source` 85, every other package at its v0.5.0 level) and fuzzes every target for 10 seconds on Linux and macOS. `task fuzz` runs the long local fuzz, and new fuzz targets cover include/exclude globs and `normalizeRepoPath`. A race test syncs 24 sources through 8 workers and checks the lock against the files on disk. Development tooling only; no CLI change.

## [0.4.0] - 2026-11-12

### Changed

- `max_parallel` is now validated: values outside 1–100 (for example `-1` or `1000`) fail config loading with exit code `2` and "invalid max_parallel". Before, they were silently accepted. **Migration:** set `max_parallel` between 1 and 100, or remove it to use the default (`4 × CPU cores`, min 10, max 16).

- `dox add` now infers `--type` from `--repo`/`--url` as the README documents (previously `Required flag "type" not set`, which broke wrappers such as `dox add my-lib --repo … --path …`), adds `--host` (validated; only `github.com` until v0.5.0) and the aliases `-r`, `-p`, `-u` and `-R` (ref). Config writes stay atomic and now keep the file's existing permission bits. `--force` replaces only sections in the simple form `add` writes and ends the section at any following table (it used to swallow a following `[display]` table). It refuses comments inside the section, multi-line values, inline tables, dotted keys and sub-tables with exit code 2 and an edit-by-hand hint. Source names must be TOML bare keys (letters, digits, `-`, `_`). **Migration:** drop `--type` from scripts if you like (it is still accepted). If `dox add --force` refuses a section, edit it by hand or delete it and run `dox add` again. Rename any source passed to `dox add` that contains other characters.

- Config validation now rejects `type = "gitlab"`, `"codeberg"` and `"git"`, and any repo source whose `host` is not `github.com`, with "… is not supported yet (planned for v0.5.0)" and exit code `2`. Before, these sources were silently sent to `api.github.com`, which failed with a misleading 404 or synced an unrelated GitHub repository that had the same `owner/repo`. The README and the `dox init` template no longer advertise GitLab, Codeberg or self-hosted support. **Migration:** remove `host`/`type` from sources that point at github.com (`host = "github.com"` is still accepted). For docs hosted elsewhere, use a `url` source for single files (for example a raw file or `llms-full.txt` URL) until v0.5.0 adds providers.

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

- README "Upgrading to v0.4.0" and "How GitHub sync works" sections. Every README command example now runs as a testscript case against local fixtures, and a guard test requires a marker on each example block so the documentation cannot drift.

- `dox version` subcommand that prints the same version, commit, and build-time metadata as `dox --version`.

- Local release preparation validates preconditions, creates a local annotated tag, runs the complete CI/snapshot/smoke checks, records binary sizes, and prints maintainer publication commands without executing them.

- Project-pinned Buildx and a temporary Docker dispatcher make local snapshot image builds reproducible without a global Docker plugin installation.

- Audit regression cases with an explicit known-failure registry; unexpected successes require removing the corresponding entries before release.

- End-to-end CLI testscript coverage and reusable fake GitHub fault-injection and JSON-golden helpers.

- A commit-pinned documentation corpus, reproducible v0.3.0 performance baselines, a generated pathological Markdown fixture, and latency/counting GitHub API/raw/archive benchmark fixtures. See `testdata/bench/README.md`.

- A shared CI quality gate, a three-OS/two-Go GitHub matrix, and `scripts/ci-local.sh` for clean-ref macOS/Linux checks plus Windows vet/test compilation while GitHub Actions is unavailable. Release publication requires the CI gate.

### Fixed

- URL sources: `dox sync --dry-run` sends a conditional `HEAD` (or a `Range: bytes=0-0` GET when the server rejects `HEAD`) and never downloads the document. Real syncs stream the body to a temporary file and rename it into place instead of buffering it in memory.

- GitHub change detection compares the configured folder's own tree SHA, fetched with the `<commit>:<path>` tree-ish. Commits outside the folder no longer force a re-sync, and monorepos fetch only that folder's tree (still 2 REST calls per source). A truncated tree is now walked directory by directory instead of failing. A missing `path` reports not found (exit code `4`). The tarball path now streams under a byte budget and falls back to raw files when the repository archive is larger than the change warrants. `tree_sha` in the lock now records the folder tree, so the first sync after upgrading re-checks each source once; only files with changed SHAs are downloaded.

- GitHub directory sync pins the ref to a commit and makes 2 REST calls per directory (commit and tree) instead of one per file. Changed files download concurrently from commit-pinned raw URLs (`download_concurrency`, default `8`), or from one commit tarball once 64 or more files changed and the repository is not much larger than the change. Unauthenticated syncs of large folders now fit GitHub's 60-request hourly quota. Measured with the fake-GitHub harness at 50 ms latency: 35 files 1.95 s → 0.37 s, 108 files 5.77 s → 0.18 s. Live, `dahlia/logtape` `docs/` (35 files) takes about 1.1 s and 2 REST calls. The lock file moves to version 2 and records `commit` beside `ref_resolved`, which is now the configured ref or `HEAD`. Version 1 lock files load unchanged.

- Network: one shared HTTP client per sync run now pools connections per host (HTTP/2 where offered), and a per-host limiter bounds concurrent requests to each host across all sources (`max_per_host`, default `16`, range 1–64). GitHub blobs are fetched with `Accept: application/vnd.github.raw` instead of base64 JSON. The pre-release `resty.dev/v3` dependency is removed in favor of `net/http`.

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
