# dox Audit — v0.3.0 (+6 unreleased commits)

| | |
|---|---|
| **Repository** | `/Users/takinprofit/Dev/dox` (`github.com/g5becks/dox`, branch `main`, clean; HEAD = `v0.3.0-6-g994a66b`) |
| **What it is** | A Go CLI that syncs library and framework docs (GitHub directories, single files, URLs such as `llms-full.txt`) into `.dox/`. It builds a manifest (outline, description) and offers `collections` / `files` / `cat` / `outline` / `search` for humans and AI agents. |
| **Audit date** | 2026-11-08 |
| **Toolchain used** | Go **1.27.3** (current); golangci-lint **2.15.0**; govulncheck (latest); `go fix` / `modernize` analyzers |
| **Primary consumer** | `agentx`: `dox.toml` with ~35 sources, 10 `mise r dox:*` wrapper tasks; `AGENTS.md` instructs agents to use dox before the web; `mise r setup` runs `dox:sync`. Installed via mise `go:github.com/g5becks/dox/cmd/dox = "latest"` (locked to 0.3.0). |
| **Size** | ~10.6k lines of Go (≈5.9k non-test) in `cmd/dox` and 8 `internal/` packages; 24 test files |

---

## 0. Executive Summary

The codebase itself is **modern and clean**. `go fix`/`modernize` find almost nothing to rewrite, tests pass under `-race`, writes are atomic (temp + rename), and govulncheck finds **no reachable vulnerabilities**. Its *version debt* is in the toolchain, release pipeline and dependency graph, not the code.

The serious problems are **behavioral**. Every item below was reproduced in a sandbox:

1. **Data loss.** `dox clean` / `sync --clean` delete whatever `output` or a source's `out` resolves to, with no containment check.
   - `output = "."` → `dox clean` **deleted the entire project directory**, source code and `dox.toml` included.
   - `out = "../../victim"` → `dox clean a` **deleted a directory outside the project**.
   - `dox.toml` is committed to repos (agentx commits one).
2. **Infinite hang.** No request or response timeout is set anywhere. A source that accepts the connection and never answers keeps `dox sync` running **forever** (killed by the test harness at 60 s). One stalled host blocks the whole run, including agentx's `mise r setup`.
3. **Concurrency setting ignored.** `--parallel` has a hard-coded default of `3` that always overrides `max_parallel` in the config and the documented "4× CPU, min 10" default. Measured: with `max_parallel = 20`, the server saw **3** concurrent requests (2.06 s); with an explicit `--parallel 20`, **12** (0.52 s).
4. **GitHub sync is slow and quota-hungry.** One REST call per file, strictly sequential, base64-in-JSON. Measured on `dahlia/logtape/docs` (35 files): **5.0 s and 37 API requests**. A single tarball took **0.36 s** and parallel raw-CDN fetches **0.79 s**, both with **0 REST quota**. A 108-file docs folder (lefthook) can't sync at all without a token (limit 60/h).
5. **Documented features don't exist.** GitLab, Codeberg and self-hosted/GHE sources pass config validation but are **sent to `api.github.com`**: misleading 404s, or silently syncing a *different* repo that happens to share the `owner/repo` name. `dox add` without `--type` fails, contradicting the README. agentx's `mise r dox:add` wrapper never passes `--type`, so it **always fails**.
6. **Every `dox sync` re-parses everything.** Manifest regeneration re-reads and re-parses **all** files of **all** sources on every sync, serially: **~600 ms, 265 MB allocated, 2.1 M allocations** on a 3,519-file / 22 MB corpus built from agentx's own sources, even when nothing changed. The Markdown parser is **superlinear and content-sensitive**: bun's 2.1 MB `llms-full.txt` parsed as Markdown took **40 s**.
7. **Errors lose all their guidance.** Errors carry **72 error codes, 29 hints and 99 context fields** through `samber/oops`. **None are ever shown** (the CLI prints only `err.Error()`), and every failure exits with status 1.

### Recommended next version
**`v0.4.0`**, in three tracks:
- **(a) Safety:** path containment for all deletes and writes, timeouts, signal-aware cancellation, the rate-limit logic fix, the `--parallel` fix.
- **(b) Platform:** Go 1.27, out-of-module tools, dependency refresh, CI with lint/test/vuln gates, release built with `go-version-file`.
- **(c) Performance:** concurrent raw/tarball GitHub fetching, a shared HTTP client, an incremental and parallel manifest, a cheap outline extractor.

Then `v0.5.0` for real GitLab/Codeberg/GHE support, a reproducible lock, and a public library and/or MCP surface. See §6.

### Baseline health (measured)

| Check | Result |
|---|---|
| `go build ./...`, `go vet ./...` | ✅ clean |
| `go test -race -cover ./...` | ✅ all pass. Coverage: config 91.0%, parser 95.6%, search 92.7%, ui 67.6%, lockfile 69.8%, manifest 69.0%, source **60.9%**, sync **27.9%**, `cmd/dox` **0%** |
| `task lint` / lefthook pre-commit `lint` | ❌ `go: no such tool "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"`, so **every commit fails** for anyone with hooks installed |
| golangci-lint 2.15.0 with the repo's own `.golangci.yml` | ❌ 40 issues (34 goconst, 5 gocognit, 1 gosec G703) |
| govulncheck | ✅ 0 reachable. ⚠️ 1 advisory in an imported package (`x/text` v0.33, GO-2026-5970, not called) and 36 in required modules: `x/net` v0.49 (linked into the binary, vulnerable packages not imported) plus mostly tool-only modules |
| `go fix -diff` / `modernize` | ✅ nothing at `go 1.25.7`; at `go 1.27` only **1** rewrite (`errors.As` → `errors.AsType[T]`) |
| `go.mod` | `go 1.25.7` (unsupported since Go 1.27's release); **412 require lines, 396 indirect**; `go.sum` 1,293 lines. The binary links only **37** modules |
| Binary | 17.6 MB (12.1 MB with `-s -w`) |
| CI | ❌ only `release.yml` (tag-triggered, **no tests**, `go-version: '1.25'`) |

### Evidence & severity legend
**[Repro]** reproduced with the built binary in a sandbox directory · **[Measured]** a number from a benchmark, profile or harness · **[Code]** confirmed by reading the source (location given) · **[Doc]** contradicts the README / `AGENTS.md`.
**Critical** data loss, a hang, or a security problem in normal use · **High** a documented feature is broken or correctness is lost · **Medium** performance or DX debt with clear user impact · **Low** polish.

---

## 1. Go Version & Ecosystem

### G1 — `go 1.25.7`: an unsupported Go line · **High** · [Measured]
Go supports the two most recent major releases. With Go **1.27** current (1.27.3 installed here), **1.25 no longer receives security fixes**.
- `go.mod` declares `go 1.25.7`, so modernizers that depend on newer language/library versions stay disabled (`go fix -diff` → empty).
- The release workflow pins `actions/setup-go` to **`go-version: '1.25'`** (`.github/workflows/release.yml`), so **shipped binaries are built with an unsupported toolchain**.

**Fix:** `go 1.27` (or `1.26` as the floor if downstream needs it) plus a `toolchain` line. Use `go-version-file: go.mod` in all workflows. Add a CI job that runs `go fix -diff ./...` and fails on output. At 1.27 it proposes exactly one change: `internal/config/types.go:249` `errors.As` → `errors.AsType[validator.ValidationErrors]`.

### G2 — The tool block bloats the module graph · **Medium** · [Measured]
The `tool (...)` block includes **goreleaser/v2, task/v3, lefthook/v2 and goimports**. Their transitive graphs (AWS/Azure/GCP SDKs, sigstore, docker, k8s kind, …) become part of the module:
- **396 indirect requirements** and a **1,293-line `go.sum`**, while the binary links **37** modules;
- slower `go mod download` in CI and Docker builds;
- **36 vulnerability reports** in required modules that dox never uses, which is noise for scanners and Dependabot;
- dependency upgrades that conflict with tool requirements.

**Fix:** move tools to a separate module (`tools/go.mod`, invoked with `go tool -modfile=tools/go.mod …`). Alternatively install them with mise, which agentx already uses, or with `go run pkg@version`. Keep only `goimports` in-module if desired. The root `go.mod` should then list about 13 direct and a few dozen indirect modules.

### G3 — The lint tool isn't installed, so lint and pre-commit are broken · **High** · [Repro]
`Taskfile.yml` runs `go tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run`, but golangci-lint is **not in the `tool` block**: `go: no such tool …`. `lefthook.yml` runs `task lint` and `task test` on **pre-commit**, so every commit fails once hooks are installed (or developers bypass hooks entirely). The strict `.golangci.yml` ("golden config" for golangci-lint v2.7.1) has therefore never been enforced. Running v2.15.0 against it gives **40 issues** (H2).
**Fix:** add golangci-lint to the tools module (G2), pinned. Run it in CI. Move `test` from pre-commit to pre-push or CI, since running `-race` tests on every commit is slow.

### G4 — Direct dependency refresh · **Medium** · [Measured]

| Module | Current | Latest | Notes |
|---|---|---|---|
| `github.com/urfave/cli/v3` | v3.6.2 | **v3.14.0** | 8 minors behind. Review the changelog for flag and help-rendering changes; C7 depends on root-level persistent flags. |
| `resty.dev/v3` | **v3.0.0-beta.6** | v3.0.0-rc.4 | A **pre-release** HTTP client in production. Consider plain `net/http`: dox needs GET, conditional headers, retry/backoff and JSON, all easy in stdlib. That would remove one dependency and give direct control over timeouts and transports (R2, P8). |
| `golang.org/x/sync` | v0.19.0 | v0.24.0 | |
| `github.com/samber/oops` | v1.21.0 | v1.23.2 | See G5. It pulls **OpenTelemetry, ULID, `samber/lo`, copystructure** into the binary. |
| `github.com/gomarkdown/markdown` | pseudo-version (2025-08-10) | pseudo-version (2026-11-07) | No tagged releases; superlinear parse cost (P4). |
| `github.com/knadh/koanf/v2` | v2.3.0 | v2.4.1 | (+ `parsers/toml/v2` v2.2.0 → v2.2.2) |
| `github.com/jedib0t/go-pretty/v6` | v6.7.8 | v6.8.3 | |
| `github.com/go-playground/validator/v10` | v10.30.1 | v10.30.6 | Heavy for ~15 struct rules; plain Go checks would be smaller and clearer (and fix R8). |
| `github.com/bmatcuk/doublestar/v4` | v4.9.1 | v4.10.4 | |
| `github.com/fatih/color` | v1.18.0 | v1.19.0 | |
| `github.com/sahilm/fuzzy` | v0.1.1 | v0.1.3 | |
| Linked `golang.org/x/net` / `x/text` | v0.49.0 / v0.33.0 | ≥ v0.62 / ≥ v0.41 | Advisories include GO-2026-5970 (`x/text`, in an imported package) and GO-2026-6629, -6610, -6611, -6612, -6617, -6860, -6866 (`x/text`/`x/net` modules). None is reachable per govulncheck; upgrade anyway, since both modules are linked into the binary. |
| Tools: goreleaser / task / lefthook / x/tools | v2.13.3 / v3.48.0 / v2.1.0 / v0.41.0 | v2.20.0 / v3.54.0 / v2.2.2 / v0.52.0 | After G2 they no longer affect the main module. |

### G5 — `samber/oops`: heavy, and its value never reaches users · **Medium** · [Measured][Code]
There are 78 `oops` call sites, setting **72 `Code(...)`, 29 `Hint(...)` and 99 `With(...)` fields**. **Nothing reads them back**: there are 0 uses of `oops.AsOops` / `.Hint()` / `.Code()` outside tests. `main` prints `fmt.Fprintln(os.Stderr, err)` (`cmd/dox/main.go`, `main()`), so hints such as "Run 'dox list' to see configured sources" are **invisible**, and every failure exits with status 1. The library still links `go.opentelemetry.io/otel{,/trace,/attribute}`, `oklog/ulid/v2`, `samber/lo` and `mitchellh/copystructure` into a CLI binary.
**Fix:** either render oops errors properly (message + hint + code, and map codes to exit codes, C5), or, preferably, replace it with a small `internal/errs` package:

```go
type Error struct{ Code Code; Msg, Hint string; Fields map[string]any; Err error }
func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }
```

This works with `errors.Is` / `errors.AsType[*errs.Error]` and `fmt.Errorf("…: %w", err)`. It removes 5+ modules from the binary.

### G6 — Modern-Go adoption: already good · **Low** · [Measured]
The code already uses range-over-int, `min`/`max`, `slices`/`maps`, generics (`sortedKeys[T]`), `errgroup.SetLimit` and `for … := range n`. Remaining opportunities:
- `errors.AsType` after the bump (G1);
- `slices.Sorted(maps.Keys(m))` instead of the hand-written `sortedKeys` (`internal/source/github.go:532`);
- `strings.SplitSeq` / `bytes.Lines` in the search and parser line loops (also an allocation win, P6);
- `t.Context()` in tests;
- `os.Root` (Go 1.24+) for the containment fix in R1. It's the idiomatic Go primitive for "operate only inside this directory".

---

## 2. Performance & Concurrency

All numbers: Apple Silicon, `GOMAXPROCS=10`, Go 1.27.3. The corpus is 3,519 files / 22 MB, taken from agentx's own sources (mise, vitest, TanStack query/router/db, arktype, bun `llms-full.txt`).

### P1 — GitHub directory sync: one sequential REST call per file · **High** · [Measured][Code]
`syncDirectory` → `downloadFiles` (`internal/source/github.go:249-272`) loops over files **serially**. Each file is `GET /repos/{o}/{r}/git/blobs/{sha}`, a JSON body with base64 content that is then `strings.ReplaceAll`'d and base64-decoded (3 copies in memory).

| Approach (dahlia/logtape `docs/`, 35 files) | Wall time | REST quota used |
|---|---|---|
| **dox today** (repo + tree + 35 blob calls, sequential) | **5.01 s** | **37** |
| No-op re-sync (tree SHA unchanged) | 0.16 s | 2 |
| One tarball from `codeload.github.com` + filtered extract | **0.36 s** | **0** |
| Parallel (8) `raw.githubusercontent.com/{o}/{r}/{ref}/{path}` (byte-identical output) | **0.79 s** | **0** |

The quota cost matters most: unauthenticated GitHub allows **60 requests/hour**. lefthook's `docs/` has **108** matching files, so **it can't sync without a token**, and agentx's ~35 sources add up to thousands of files.
**Fix:** keep the tree call for change detection (1 request), then fetch changed files via **raw CDN, pinned to the resolved commit SHA**, with bounded concurrency (8–16). Use `codeload` tarballs when the changed set is large relative to the repo. Request blobs with `Accept: application/vnd.github.raw` if the API path remains (no JSON/base64).

### P2 — Effective concurrency is always 3 · **High** · [Repro][Code]
- `cmd/dox/main.go`: `const defaultParallel = 3` and `&cli.IntFlag{Name: "parallel", …, Value: defaultParallel}`.
- `syncAction` always passes `MaxParallel: cmd.Int("parallel")`, which is `3` unless the flag is given.
- `internal/sync/sync.go:100-107` uses `cfg.MaxParallel` / `getDefaultMaxParallel()` (max(4×CPU, 10)) **only if `MaxParallel <= 0`**, so that code is dead.

Repro: 12 URL sources against a local server with a 500 ms response delay that counts concurrent requests:

| Invocation | Max concurrent requests | Wall time |
|---|---|---|
| `max_parallel = 20` in config, no flag | **3** | 2.06 s |
| `--parallel 20` | 12 | 0.52 s |

The README, the `init` template ("default: 4x CPU cores, min 10") and agentx's `dox.toml` (`max_parallel = 20`) are all ineffective.
**Fix:** flag default `0` (meaning unset), with precedence flag > config > computed default. Add a unit test for that precedence. Revisit the default itself: 4×CPU (40 on this machine) sources × per-source download concurrency (P1) can trip GitHub's secondary rate limits. Add a **global** HTTP concurrency limiter (e.g. a semaphore of 16 per host) instead of multiplying per-source pools.

### P3 — Manifest regeneration: full, serial, on every sync · **High** · [Measured][Code]
`manifest.Generate` (`internal/manifest/generator.go`) runs after every non-dry-run sync, including syncs where everything was skipped. It walks **every** source directory, does `os.Stat` + `os.ReadFile` for every file, picks a parser and fully parses it, then `MarshalIndent`s the result. It ignores `ctx`.

| Measurement (3,519 files / 22 MB) | Result |
|---|---|
| `manifest.Generate` (current) | **~600 ms, 265 MB allocated, 2.14 M mallocs** per run |
| Prototype: parallel per-file parse within each source (errgroup, GOMAXPROCS) | ~385 ms (−36%); same allocations |
| CPU profile | **48%** in `gomarkdown` `Parser.Parse` (table/code-span handling), **30%** in file-read syscalls |
| Top allocators | `json.MarshalIndent` re-indent path (~155 MB cumulative over 7 runs), `os.ReadFile`, gomarkdown AST nodes |

**Fix, in order of impact:**
1. **Incremental generation.** Reuse the previous manifest's entries for files whose lock SHA (GitHub) or size+mtime (URL/other) are unchanged, and only (re)parse changed files and sources. A no-op sync should do near-zero parsing.
2. **Parallel parsing** across *all* files of all sources, with deterministic output order (sort before save).
3. **A cheap outline extractor** (P4) instead of a full CommonMark AST.
4. Use `d.Info()` from `WalkDir` instead of a second `os.Stat`. Honor `ctx`.

### P4 — Markdown parsing is superlinear; a 2.1 MB document takes 40 s · **High** · [Measured]
Parsing prefixes of a real doc (bun's `llms-full.txt`) with the Markdown parser:

| Input | 64 KB | 128 KB | 256 KB | 512 KB | 2.1 MB |
|---|---|---|---|---|---|
| `MarkdownParser.Parse` | 2.5 ms | 3.5 ms | 25.6 ms | 185.1 ms | **40,070 ms** |

The cost is content-dependent: tanstack-db's 1.0 MB `oracle-coverage.md` parses quickly. A large table- or code-span-heavy `.md`/`.mdx` (CHANGELOGs, vendor `llms-full.md`, generated API references) can therefore stall **every** sync and `dox outline`. The only guard is `maxParseSize = 50 MB`. The `.txt` path takes ~0 ms.
**Fix:** dox needs only headings, the first paragraph or description, and front-matter. A **single-pass line scanner** gets these in linear time: ATX/setext headings, skipping fenced code blocks, a front-matter block, and the first non-empty paragraph. Keep a full parser only if a feature truly needs an AST, and if so prefer **goldmark** (linear-time, widely used, tagged releases). Add a hard per-file time/size budget (e.g. full parsing ≤ 256 KB, line scan above) and a regression benchmark using a pathological fixture.

### P5 — Manifest/lock JSON encoding · **Low** · [Measured]
`json.MarshalIndent` on the 5.8 MB manifest takes **11.2 ms**; compact `json.Marshal` + write takes **3.7 ms**. Under Go 1.27's json/v2-backed `encoding/json`, `MarshalIndent` marshals and then re-indents (`jsontext.AppendFormat` / `reformatValue` dominate allocations). Write compact JSON, or stream with `json.NewEncoder(w)` + `SetIndent` into a buffered temp file. The manifest is machine-read, and `dox` commands can pretty-print on demand.

### P6 — Content search: per-line allocations, serial file scan · **Medium** · [Measured][Code]
`search.Content` (`internal/search/content.go`): per file it does `os.Stat` + `os.ReadFile` + `strings.Split(string(content), "\n")`, which copies the file and allocates every line. For literal queries it does `strings.ToLower(line)` on **every line** (an allocation per line).

| Query over 22 MB | Time | Allocated |
|---|---|---|
| literal `"query"` (35,348 hits) | 99 ms | **94 MB** |
| regex `use[A-Z]\w+` (7,808 hits) | 231 ms | 62 MB |
| metadata fuzzy `"routing"` | 10.9 ms | 31.7 MB |

**Fix:** search on `[]byte` with `bytes.Lines`. For case-insensitive literals, lowercase each file **once** (or use a precompiled `(?i)` literal with `regexp`'s literal prefix optimization), and convert to `string` only for matching lines. Scan files with a bounded worker pool, merging results in deterministic order, and keep the early exit on `--limit`. Expect a ~3–5× reduction in allocations. Add a corpus benchmark (the existing `BenchmarkContentSearch700Files` uses tiny synthetic files, so it couldn't show this).

### P7 — The change-detection check rarely skips; whole-repo recursive trees · **Medium** · [Code]
- `syncDirectory` compares `prevLock.TreeSHA == tree.SHA`, where `tree` is the **recursive tree of the whole repo** at `ref`. Any commit anywhere changes it, so active repos never take the fast path even when `docs/` is untouched. The per-file SHA diff still avoids re-downloads, but every sync pays for the full recursive tree.
- Fetching a recursive tree for a monorepo in order to read one subfolder is wasteful, and a **truncated** tree (>100k entries / 7 MB) is a **hard failure** ("contents fallback is not implemented").

**Fix:** resolve the **subtree SHA** of `path` (from the parent tree, or `GET /repos/{o}/{r}/contents/{parent}?ref=…`). Compare and store that. Fetch `git/trees/{subtreeSHA}?recursive=1` for just the docs folder, which also removes most truncation cases. Implement the truncation fallback by walking non-recursive subtrees.

### P8 — HTTP clients and connection reuse · **Medium** · [Code]
`source.New` creates a **new resty client (new `http.Transport`) per source** (`newGitHubClient`, `NewURL`), so ~35 sources means ~35 TLS handshakes to `api.github.com` with no HTTP/2 connection sharing.
**Fix:** one shared `*http.Client` (or one per host) owned by `sync.Run`, with explicit timeouts (R2), `MaxIdleConnsPerHost`, and a per-host concurrency limiter (P2).

### P9 — `--dry-run` downloads URL sources fully · **Low** · [Code]
`urlSource.Sync` performs the full GET and `io.ReadAll(response.Body)` even when `opts.DryRun` (`internal/source/url.go`). Use a conditional `HEAD` (or GET with `Range: bytes=0-0`) in dry-run, and stream to the temp file instead of reading into memory for real syncs (bun's `llms-full.txt` is 2.1 MB; others are larger).

---

## 3. CLI & Library Ergonomics

### C1 — There is no public Go API · **Medium** · [Code]
Everything is under `internal/` (`config`, `lockfile`, `manifest`, `parser`, `search`, `source`, `sync`, `ui`), so **nothing is importable** by other Go programs. The `sync.Options{OnEvent}` callback and `search` functions are well-shaped for embedding. Since dox's main users are AI agents (agentx's `AGENTS.md` mandates it), two surfaces would be valuable:
- **A small public package** (e.g. `github.com/g5becks/dox/dox`), a stable facade: `Load(configPath) (*Project, error)`, `(*Project).Sync(ctx, SyncOptions) (*SyncResult, error)` with events, `Search(ctx, Query) iter.Seq2[Result, error]`, `Cat(ctx, Collection, Path, Range)`. Keep `internal/` for implementation.
- **`dox mcp`**: a stdio MCP server exposing `collections`, `files`, `outline`, `cat` and `search` as tools, so agents don't have to shell out through `mise` wrappers and parse JSON.

Decide explicitly. If the answer is "CLI only", say so in the README so consumers don't expect a library.

### C2 — `--parallel` default overrides config · **High** · [Repro] → see P2.

### C3 — `dox add`: README behavior, the agentx wrapper and the code disagree · **High** · [Repro][Doc]
- The README says "GitHub source (type inferred from --repo)": `dox add goreleaser --repo goreleaser/goreleaser --path www/docs`. The binary answers **`Required flag "type" not set`** (`--type` is `Required: true` in `newAddCommand`).
- The README's GitLab example uses **`--host gitlab.com`**, but `add` has **no `--host` flag**.
- agentx's `mise r dox:add` wrapper builds `dox add <name> --repo … --path …` and **never passes `--type`**, so it **always fails**. `AGENTS.md` tells agents to use it.
- No short aliases exist for `--repo`/`--path`/`--url` (`-r` → "flag provided but not defined: -r").

**Fix:** infer the type from `--repo`/`--url` (as config loading already does in `ApplyDefaults`); make `--type` optional; add `--host`; add aliases `-r`, `-p`, `-u`, `-R` (ref). Add CLI tests (T4).

### C4 — GitLab, Codeberg and self-hosted sources are documented but not implemented · **High** · [Repro][Code][Doc]
- Config accepts `type = github|url|git|gitlab|codeberg` and a `host` (`internal/config/types.go:107-109, 206-241`), and normalizes `host` to a type. The README documents GitLab, Codeberg and "Self-hosted (GitHub Enterprise, GitLab CE/EE, Gitea, etc.)".
- `source.New` (`internal/source/source.go:38-52`) only knows `github` and `url`. The GitHub client hardcodes `https://api.github.com` and ignores `host` (`internal/source/github.go:26, 671-685`).

Repro: sources with `host = "gitlab.com"`, `"codeberg.org"` and `"github.mycorp.com"` all fail with **"github API returned status 404 for repository metadata"**. If a repo with the same `owner/repo` exists on github.com, dox would **silently sync the wrong repository**.
**Fix (short term, v0.4.0):** reject non-github hosts at validation with a clear "not yet supported" error, and remove the claims from the README. **(v0.5.0):** implement providers behind the `Source` interface: GitHub (+ Enterprise base URL), GitLab (`/api/v4/projects/:id/repository/tree` + `/files/:path/raw`, or archive), Gitea/Forgejo/Codeberg (`/api/v1/repos/{o}/{r}/…`), each with tests against `httptest` servers.

### C5 — Errors: hints hidden, exit codes undifferentiated · **High** · [Repro][Code]
- `dox sync nope` prints `source "nope" not found in config`; the attached hint "Run 'dox list' to see configured sources" is never shown (G5).
- Every failure exits `1`: config errors, network errors, partial sync failures and "not found" alike. Agents and scripts can't tell "fix your config" from "retry later".
- A wrong-typed config value (`repo = 5`) is reported as **`missing 'path' for source "x"`** (R8).

**Fix:** a single error renderer (message, `hint:` line, `--verbose` for context fields) and documented exit codes: 0 ok; 1 runtime/network; 2 usage/config; 3 partial sync failure; 4 not found. Add `--json` error output for agent consumers: `{"error":{"code":…,"message":…,"hint":…}}`.

### C6 — `search` line numbers are 1-based; `cat --offset` is 0-based · **Medium** · [Repro]
`dox search queryKey --content --json` reports `line: 152`. `dox cat tanstack-db collections/local-only-collection.md --offset 152 --limit 1` prints the **next** line; `--offset 151` prints the hit. Agents following agentx's `AGENTS.md` workflow (search, then cat) are off by one.
**Fix:** make `cat` accept a 1-based `--line N` / `--from N` (or `path:line` syntax) and deprecate the 0-based `--offset`. Make `outline` line numbers, search results and `cat` all use 1-based numbering.

### C7 — Flag and command consistency · **Medium** · [Repro]
- `--config/-c` is redefined on every subcommand and missing on `init`. Make it a **root persistent flag** (urfave/cli v3 supports this) and add `DOX_CONFIG` env support.
- Output selection is inconsistent: every read command has `--json`, but only `files`/`search` have `--format table|json|csv`. Standardize on `--format` (`--json` as an alias) everywhere. Add `--format jsonl` for streaming search results.
- `--limit 0` means "all" for `collections`/`search` but "config default" for `files`, which has a separate `--all`. Use one convention.
- `outline`/`cat` take `<collection>` and a file argument, with no fuzzy fallback or "did you mean" on misses.
- Missing commands agents would use: `dox doctor` (config + token + rate-limit + output checks), `dox sync --check` (exit non-zero if stale, for CI), `dox completion`. urfave/cli v3 ships shell completion.
- `version` prints `dev (commit unknown, built unknown)` for `go install` builds. Use `runtime/debug.ReadBuildInfo()` (`vcs.revision`, `vcs.time`, `Main.Version`) as the fallback.

### C8 — No cancellation or timeouts · **Critical** · [Repro] → see R2 / R4.

### C9 — The lockfile doesn't lock · **Medium** · [Repro][Code]
After syncing `dahlia/logtape` with no `ref`, `.dox/.dox.lock` records **`"ref_resolved": "main"`** (the branch name), not the commit SHA. Two machines syncing the same lock can get different content, and there's no way to reproduce yesterday's docs.
**Fix:** resolve the branch to a **commit SHA** (`GET /repos/{o}/{r}/commits/{ref}` or the tree response's commit) and store both `ref` and `commit`. Fetch content by commit (also required for consistent raw-CDN fetching, P1). Add `dox sync --locked`: fetch exactly the lock's commits and fail if the config changed. That's the analogue of `npm ci`, and useful for agentx's CI and `setup`.

### C10 — `dox add` edits TOML as text · **Low** · [Code]
`appendSourceSection` / `replaceSourceSection` (`cmd/dox/main.go`) splice strings into the config file. They handle simple cases but can't safely deal with inline tables, dotted keys, arrays of tables, or comments inside a section. gosec flags the write (G703, `main.go:513`).
**Fix:** use a format-preserving TOML editor, or limit `add` to appending and refuse `--force` replacement when the section isn't in a recognized simple form. Write atomically (temp + rename), as the lockfile already does.

---

## 4. Testing & Robustness

### R1 — `clean` and `sync --clean` can delete arbitrary directories · **Critical** · [Repro][Code]
- `cleanAction` (`cmd/dox/main.go:637-`) calls `os.RemoveAll(outputDir)` for a full clean, or `os.RemoveAll(<per-source dir>)`. `sync.Run` (`internal/sync/sync.go:81-88`) does the same for `--clean`.
- `output` is validated only as `dirpath` (`internal/config/types.go:97`), which accepts `"."`, `".."` and `"/"`. Per-source `out` (`:116`) and URL `filename` are **not validated at all**.

Sandbox repro:

| Config | Command | Result |
|---|---|---|
| `output = "."` | `dox clean` | **the whole project directory deleted** (source files and `dox.toml`) |
| `out = "../../victim"` (with `output = ".dox"`) | `dox clean a` | **a sibling directory outside the project deleted** |

`dox.toml` is committed in consumer repos, so a careless or malicious edit turns a routine `dox clean` (or agentx's `mise r dox:clean`) into deleting arbitrary directories. `deleteStaleFiles` and downloads write under the same unvalidated roots.
**Fix:**
- `output` must resolve **inside the config directory** and must not equal it (`filepath.IsLocal` on the relative path; refuse `.`, `..` and absolute paths unless `--allow-external-output` is set).
- `out` and `filename` must be **local relative paths** (`filepath.IsLocal`).
- Perform all writes and deletes through an **`os.Root`** opened at the output directory (Go 1.24+), so containment is enforced by the OS-level API.
- Before `RemoveAll`, require the target to contain a dox marker (`.dox.lock` or a `.dox-root` sentinel); otherwise refuse.

Add tests for each case.

### R2 — No HTTP timeouts, so a stalled server hangs dox indefinitely · **Critical** · [Repro][Code]
Neither client sets a request or response timeout. resty v3 defaults only set dial (30 s) and TLS-handshake (10 s) timeouts; `ResponseHeaderTimeout` and the overall timeout stay unset. Repro: a TCP listener that accepts and never replies leaves `dox sync` **blocked until killed** (at 60 s by the harness). Because `errgroup.Wait` waits for every source, one bad host blocks the whole run and the lockfile is never saved.
**Fix:** a per-request timeout (e.g. 30 s default, configurable `timeout = "30s"`), `ResponseHeaderTimeout`, an overall per-source deadline via `context.WithTimeout`, and an `--timeout` flag. Add an `httptest` server test that never responds, and assert the sync fails within the deadline.

### R3 — Rate-limit handling is inverted · **High** · [Code]
`checkRateLimit` (`internal/source/github.go:687-713`) runs **only after** `!response.IsSuccess()` has returned an error:
1. When GitHub actually rate-limits (HTTP **403/429**), the user gets the generic "github API returned status 403 for blob". The `GITHUB_RATE_LIMIT` error and its "Set github_token, GITHUB_TOKEN, or GH_TOKEN" hint are never produced.
2. A **successful** response whose `X-RateLimit-Remaining` is `0` (the last allowed call) is turned into an **error**, failing a sync that just received its data.
3. `warnedLowRL` is set when ≤ 10 calls remain but is **never used** to warn.
4. resty retries (count 3, waits 1–5 s) don't honor `Retry-After` or `X-RateLimit-Reset`, and retrying 403 rate-limit responses wastes quota.

**Fix:**
- Classify responses first: 403/429 with `X-RateLimit-Remaining: 0` or `Retry-After` → `RateLimited{Reset}`. Wait if the reset is within a configurable bound, else fail with the token hint.
- Treat `remaining == 0` on a 2xx as a warning and success.
- Surface the low-quota warning through the sync event stream.
- Retry only idempotent 5xx/network errors, with jittered backoff and `Retry-After` respected.

### R4 — No signal handling or cancellation · **Medium** · [Code]
`run` uses `context.Background()`, with no `signal.NotifyContext`. Ctrl-C kills the process mid-run. Thanks to atomic writes no file is corrupted, but **the lockfile and manifest aren't saved**, so completed work is forgotten and re-done next time. `manifest.Generate` ignores `ctx`.
**Fix:** `ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. On cancellation, finish or abort in-flight writes, **save the lock entries of completed sources**, then exit with code 130. Pass `ctx` through the manifest and search loops.

### R5 — Coverage is lowest where the risk is highest · **High** · [Measured]

| Function | Coverage |
|---|---|
| `cmd/dox` (all commands, ~1,700 lines) | **0%** |
| `sync.Run` | **4.2%** |
| `sync.syncSource`, `processResults`, `getDefaultMaxParallel` | 0% |
| `source.(*githubSource).downloadFiles`, `deleteStaleFiles`, `cleanupEmptyDirs`, `newGitHubClient` | 0% |
| `source.(*githubSource).resolveRef` | 11.1% |
| `source.(*githubSource).checkRateLimit` | 25.0% |
| `source.New` | 0% |

Well covered: config (91%), parser (95.6%), search (92.7%). Every Critical/High behavioral bug above sits in an untested function.

### R6 — Missing test classes · **High**

| Class | Tooling | Would have caught |
|---|---|---|
| **CLI end-to-end** (golden output, exit codes, flags) | `rogpeppe/go-internal/testscript` (`.txtar` scripts) | C2/P2, C3, C5, C6, R1 |
| **HTTP behavior** against real servers | `net/http/httptest`: stalls, 403/429 + headers, ETag/304, truncated trees, redirects | R2, R3, P7, C9 |
| **Path containment** (property-style) | table tests + `os.Root` | R1 |
| **Fuzzing** | `go test -fuzz`: parser outline extraction, glob include/exclude (`shouldIncludeFile`), `normalizeRepoPath`, TOML `add` editing | P4 (pathological inputs), C10 |
| **Corpus benchmarks** | `testing.B` + a checked-in or generated multi-MB fixture | P3, P4, P6 |
| **Concurrency** | `-race` with real parallel sync of N fake sources (`httptest`) + lock assertions | P2, R4 |

The current GitHub tests use a mock `RoundTripper` (`internal/source/export_test.go:77`, base URL `https://api.github.test`). Keep it for unit tests, but add `httptest` for timeout, header and streaming behavior.

### R7 — Error-handling idioms · **Medium** · [Measured]
`oops` is used at 78 sites, `fmt.Errorf` at 1, and `errors.Is`/`As` at 6. There are no sentinel errors and no `%w` chains. Callers (including a future public API, C1) can't programmatically distinguish "source not found" from "network". After G5:
- export sentinel and typed errors (`ErrSourceNotFound`, `*RateLimitError{Reset time.Time}`, `*HTTPStatusError{Code int}`);
- wrap with `%w`;
- test with `errors.Is` / `errors.AsType`;
- map them to exit codes in one place (C5).

### R8 — Validation misreports type errors · **Low** · [Repro]
A config with `[sources.x] repo = 5` reports `missing 'path' for source "x"`. The type mismatch is lost during koanf unmarshalling (weakly typed decode) and validation runs on zero values. Use strict decoding (`koanf` with `mapstructure` `ErrorUnused`/`WeaklyTypedInput: false`) and report the field, expected type and line.

### R9 — Concurrent invocations · **Low** · [Code]
Two simultaneous `dox sync` runs (editor integration + `mise r setup`) race on `.dox.lock` and `manifest.json`. Writes are atomic, but last writer wins and entries are lost. Take an advisory file lock (`.dox/.lock.pid` / `flock`) for sync and clean.

### R10 — The Docker image can't make HTTPS requests · **High** · [Code — not executed; Docker daemon was not running]
`Dockerfile` is `FROM scratch` + `COPY dox`. A `CGO_ENABLED=0` Go binary on Linux loads CA roots from system paths (`/etc/ssl/certs/…`), which don't exist in `scratch`. No code imports `golang.org/x/crypto/x509roots/fallback`, and `.goreleaser.yaml` adds no certificate files. So `docker run ghcr.io/g5becks/dox sync` should fail every HTTPS request with `x509: certificate signed by unknown authority`.
**Fix:** `FROM gcr.io/distroless/static:nonroot` (includes CA certificates and a non-root user), or `COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/`, or blank-import `golang.org/x/crypto/x509roots/fallback`. Add a container smoke test in CI (`docker run … sync` against a public URL).

---

## 5. Code Hygiene & Chores

| ID | Item | Evidence | Action |
|---|---|---|---|
| H1 | Lint tool missing; pre-commit blocked | G3 | Tools module + pinned golangci-lint; CI lint job. |
| H2 | 40 lint findings under the repo's own config | golangci-lint 2.15: 34 `goconst` (CLI literals in `cmd/dox/{files,search,collections,outline,main}.go`), 5 `gocognit` (`config.Validate` 27, `sync.Run` 23, three integration tests), 1 `gosec` G703 | Extract output-format constants/enums; split `Validate` per section and `Run` into plan/execute/commit; justify or fix G703 (C10). |
| H3 | No CI for tests/lint/vuln; release runs no tests; actions pinned to mutable tags; `go-version: '1.25'`; goreleaser `version: '~> v2'` floating | `.github/workflows/release.yml` | `ci.yml` (Linux/macOS/Windows × Go 1.26/1.27: `go vet`, `golangci-lint`, `go test -race -cover`, `govulncheck`, `go fix -diff`); release job `needs: ci`; pin actions by SHA; `go-version-file`; pin goreleaser. |
| H4 | Path resolution duplicated 4× | `cmd/dox/main.go` `resolveOutputRoot`, `internal/sync/sync.go` `resolveOutputRoot` + `resolveSourceOutputDir`, `config.(*Config).OutputDir`, `manifest.resolveSourceDir` | One `config.Paths` type (output root, per-source dir) that enforces R1 containment; delete the copies. |
| H5 | Dead code | `warnedLowRL` never read (R3); `getDefaultMaxParallel` unreachable from the CLI (P2) | Remove or wire up. |
| H6 | Misleading `init` template and README | "Max parallel source syncs (default: 4x CPU cores, min 10)"; GitLab/Codeberg/self-hosted sections; `dox add` type inference; `--host` | Fix with the code changes; add a docs test that runs README examples via testscript. |
| H7 | Release hygiene | 6 commits since v0.3.0, 5 of them "chore: sync beads export state"; agentx consumes `latest` → 0.3.0 | Keep tracker-sync commits out of `main` (squash, or a separate branch); adopt release-please or changesets; publish a CHANGELOG. |
| H8 | Docker image cannot do TLS | R10 | distroless/static base + smoke test. |
| H9 | Binary size 17.6 MB (12.1 MB stripped) | measured | After G5 (drop oops/otel/lo/ulid), optional removal of resty/validator, and `-trimpath -ldflags="-s -w"` (already in goreleaser), re-measure; expect a few MB less. |
| H10 | `CONTRIBUTING.md` / `AGENTS.md` | 292 / 75 lines | Update them to the new toolchain commands (`go tool -modfile=tools/go.mod …`) and test strategy. |

---

## 6. Action Plan / Roadmap

> **Process note for the implementing agent:** issue tracking in this repo has used beads (see the commit history). Track these tasks there or as GitHub issues; the IDs `DX-xx` are stable references. Each bug-fix task starts by turning its sandbox repro (Appendix A) into a failing test: `testscript` for CLI behavior, `httptest` for network behavior.

### Release strategy

| Release | Scope | Compatibility |
|---|---|---|
| **v0.4.0** | Phases 0–2 | **Behavior changes:** unsafe `output`/`out`/`filename` values are rejected; non-GitHub hosts are rejected until supported; `--parallel` precedence fixed; new exit codes; `cat --offset` deprecated in favor of 1-based `--line`. Ship with a migration note. |
| **v0.5.0** | Phase 3 | Additive: GitLab/Gitea/Codeberg/GHE providers, `--locked`, public API and/or `dox mcp`. |

agentx follow-ups are listed at the end.

### Phase 0 — Platform & guardrails (do first)

| ID | Task | Fixes | Acceptance criteria |
|---|---|---|---|
| DX-01 | Go 1.27 | G1, G6 | `go 1.27` + `toolchain go1.27.x` in `go.mod`; `go fix ./...` applied (the `errors.AsType` change); `go fix -diff ./...` clean in CI. |
| DX-02 | Tools out of the main module | G2, G3, H1 | `tools/go.mod` with golangci-lint, goreleaser, task, lefthook, goimports (pinned); root `go.mod` has no `tool` block and ≤ ~60 requirements; `task lint` works; `go mod tidy` clean. |
| DX-03 | CI pipeline | H3 | `ci.yml`: matrix {ubuntu, macos, windows} × {Go 1.26, 1.27}: `go vet`, `golangci-lint run`, `go test -race -coverprofile`, `govulncheck`, `go fix -diff`; coverage reported; release workflow `needs` CI, uses `go-version-file: go.mod`, actions pinned by SHA, goreleaser pinned. |
| DX-04 | Dependency refresh | G4 | All direct deps at latest (urfave/cli v3.14+, x/sync, koanf, go-pretty, validator, doublestar, color, fuzzy); `x/net` ≥ v0.62, `x/text` ≥ v0.41; resty at least at rc (or removed by DX-21); govulncheck "modules you require" count near 0 after DX-02. |
| DX-05 | Lint to zero | H2 | `golangci-lint run` passes with the repo config; `config.Validate` and `sync.Run` under cognitive complexity 20; constants for output formats. |
| DX-06 | Hooks | G3 | Pre-commit: `gofmt`/`goimports` + lint on staged files only; tests on pre-push. |

### Phase 1 — Safety & correctness (release-blocking for v0.4.0)

| ID | Task | Fixes | Depends | Acceptance criteria |
|---|---|---|---|---|
| DX-10 | Path containment | R1, H4 | DX-01 | One `Paths` type; `output` must be local to the config dir and ≠ it; `out`/`filename` must pass `filepath.IsLocal`; all writes and deletes go through `os.Root`; `RemoveAll` only on directories containing `.dox.lock` (or a sentinel); testscript cases for `output="."`, `".."`, `"/"`, `out="../../x"`, `filename="../x"` all refuse with exit code 2 and delete nothing. |
| DX-11 | Timeouts & cancellation | R2, R4, C8 | DX-10 | Shared HTTP client with request + response-header timeouts (configurable `timeout`, `--timeout`); `signal.NotifyContext` in `main`; on cancel, completed sources' lock entries are saved and exit code is 130; an `httptest` never-responding server makes `sync` fail within the configured timeout (test). |
| DX-12 | Rate-limit correctness | R3 | DX-11 | 403/429 with rate-limit headers → typed `RateLimitError` with reset time and token hint; 2xx with remaining 0 succeeds and emits a warning event; low-quota warning shown once; retries only on 5xx/network, honoring `Retry-After`; `httptest` tests for each case. |
| DX-13 | `--parallel` precedence | P2, C2, H5 | — | Flag default 0; precedence flag > `max_parallel` > computed default; unit test; the local-server repro shows `max_parallel = 20` → 12 concurrent for 12 sources. |
| DX-14 | Visible errors & exit codes | C5, G5, R7 | — | Renderer prints message + `hint:`; `--verbose` adds context; documented exit codes (0/1/2/3/4, 130); `--json` error object; typed/sentinel errors usable with `errors.Is`/`AsType`; testscript coverage for each code. |
| DX-15 | Honest host support | C4 | — | Config validation rejects non-GitHub hosts and the `gitlab`/`codeberg`/`git` types with "not supported yet" (until DX-30); README updated; test proves `host = "gitlab.com"` never contacts `api.github.com`. |
| DX-16 | `dox add` UX | C3, C10 | DX-14 | Type inferred from `--repo`/`--url`; `--type` optional; `--host` (validated per DX-15); aliases `-r -p -u -R`; atomic write; `--force` refuses unrecognized section shapes; README examples pass as testscript; agentx's `mise r dox:add my-lib -r owner/repo -p docs` works unchanged. |
| DX-17 | Docker TLS | R10, H8 | DX-03 | distroless/static (or CA bundle) base; CI container smoke test runs `dox sync` against an HTTPS URL successfully. |
| DX-18 | Consistent line numbering | C6 | — | `cat --line N` (1-based) and `path:line` accepted; `--offset` deprecated with a warning; search, outline and cat all use 1-based lines; testscript round-trip (search hit → cat) returns the same text. |

### Phase 2 — Performance (v0.4.0)

| ID | Task | Fixes | Acceptance criteria |
|---|---|---|---|
| DX-20 | Concurrent, quota-free GitHub fetching | P1, C9 | Changed files fetched via raw CDN pinned to the **commit SHA** with a bounded pool (default 8, configurable), or via codeload tarball when the changed set is large; API used only for ref→commit and subtree; benchmark on a 35-file docs dir ≤ 1.0 s (from 5.0 s) and ≤ 3 REST calls (from 37); a 108-file dir syncs unauthenticated. |
| DX-21 | Shared HTTP layer | P8, P2, G4 | One client per host with connection reuse, per-host concurrency limit, retries per DX-12; resty removed or isolated (decision recorded); the base64 JSON blob path removed or switched to `Accept: application/vnd.github.raw`. |
| DX-22 | Subtree change detection | P7 | Lock stores the subtree SHA of `path`; unchanged docs folder → skip with 1–2 API calls even when the repo has new commits; truncated trees handled by non-recursive traversal (test with `httptest`). |
| DX-23 | Incremental + parallel manifest | P3, P5 | Unchanged files reuse prior entries; parsing parallel across all files with deterministic output; `ctx` honored; a no-op sync on the 3,519-file corpus spends ≤ 50 ms in manifest work (from ~600 ms); compact or streamed JSON. |
| DX-24 | Linear-time outline extraction | P4 | Line-scanner for headings, description and front-matter (fenced-code aware), used by default; full parser (goldmark, if kept) only under a size budget; benchmark: 2.1 MB pathological Markdown ≤ 100 ms (from 40 s); fuzz test for the scanner. |
| DX-25 | Faster content search | P6 | `[]byte` + `bytes.Lines`, case-folding once per file, bounded parallel scan with ordered merge; corpus benchmark: literal search allocations ≤ 30 MB (from 94 MB) and ≤ 50 ms (from 99 ms) on 22 MB. |
| DX-26 | Dry-run without downloads | P9 | URL dry-run uses conditional `HEAD`; real downloads stream to the temp file; test asserts no body is read in dry-run. |

### Phase 3 — Features & API (v0.5.0)

| ID | Task | Fixes | Acceptance criteria |
|---|---|---|---|
| DX-30 | Git hosting providers | C4 | GitHub Enterprise (custom API base), GitLab (gitlab.com + self-hosted), Gitea/Forgejo/Codeberg providers behind `Source`; per-host tokens (`GITLAB_TOKEN`, `GITEA_TOKEN`, config `tokens.{host}`); `httptest` contract tests per provider; README sections restored. |
| DX-31 | Reproducible lock | C9 | Lock stores `ref` + `commit`; `dox sync --locked` reproduces exactly (fails if the config diverges); `dox sync --check` exits non-zero when sources are stale. |
| DX-32 | Public package and/or MCP server | C1 | Decision recorded. If yes: `github.com/g5becks/dox/dox` facade with `Sync`/`Search`/`Cat`/`Outline`, iterator-based results, typed errors, and examples (`Example_*` tests); and/or `dox mcp` stdio server exposing the read commands as tools with JSON schemas. |
| DX-33 | CLI polish | C7 | Root persistent `--config` + `DOX_CONFIG`; `--format` everywhere (`table|json|jsonl|csv`); unified `--limit` semantics; `dox doctor`; shell completion; `version` falls back to `debug.ReadBuildInfo`. |
| DX-34 | Robust config | R8, G4 | Strict decoding with field/type/line in errors; validator replaced by explicit checks (optional); fuzz test for config loading. |
| DX-35 | Concurrency safety | R9 | Advisory lock on the output dir for `sync`/`clean`; a second invocation waits or fails fast with a clear message. |
| DX-36 | Test-suite completion | R5, R6 | `cmd/dox` ≥ 70% via testscript; `sync` ≥ 80%; `source` ≥ 85%; fuzz targets for parser, globs, path normalization and TOML editing run in CI (short budget) and nightly (long). |

### Downstream follow-ups for agentx

1. **`dox.toml`:** `max_parallel = 20` starts working after DX-13. Keep it, or lower it once DX-20 adds per-source concurrency (watch GitHub's secondary rate limits).
2. **Tokens:** until DX-20 ships, configure `GITHUB_TOKEN` for `mise r dox:sync`. Unauthenticated syncs of agentx's ~35 sources exceed GitHub's 60 requests/hour.
3. **Wrappers:** `mise r dox:add` works after DX-16 without changes. Optionally pass `--json`/exit codes through to agents.
4. **AGENTS.md:** after DX-18, document 1-based `--line` for `dox:cat`, and the exit codes from DX-14.
5. **Version pin:** agentx installs `go:github.com/g5becks/dox/cmd/dox = "latest"` (locked 0.3.0). Pin `0.4.0` explicitly once released, and consider `dox sync --locked` (DX-31) in `setup`/CI.
6. **`.dox/` safety:** keep `output = ".dox"`. After DX-10, unsafe values are rejected anyway.

---

## Appendix A — Reproductions

All of these ran against the binary built from `main` (`go build -o …/dox-bin ./cmd/dox`), in throwaway directories under the audit scratchpad (`/private/tmp/claude-501/-Users-takinprofit-Dev/239a07a4-30ee-4777-aea1-feeee3c1317b/scratchpad/`). The scratchpad is temporary; the essential steps are below.

```bash
# R1 — destructive clean (run ONLY in a throwaway directory)
mkdir -p sandbox/project/src && cd sandbox/project && echo important > src/code.go
printf 'output = "."\n[sources.a]\nurl = "https://example.com/llms.txt"\n' > dox.toml
dox clean                       # → sandbox/project is gone
# variant: output = ".dox" + [sources.a] out = "../../victim" ; dox clean a  → sandbox/victim is gone

# R2 — hang on a server that never answers
bun -e 'Bun.listen({hostname:"127.0.0.1",port:47920,socket:{data(){},open(){}}})' &
printf 'output = ".dox"\n[sources.stall]\nurl = "http://127.0.0.1:47920/llms.txt"\n' > dox.toml
timeout 60 dox sync; echo $?    # → 124 (still hung at 60 s)

# P2 — effective concurrency (local server counts concurrent requests; 500 ms delay)
# 12 URL sources + max_parallel = 20 → max concurrent 3, 2.06 s ; with --parallel 20 → 12, 0.52 s

# C3 — dox add
dox add mylib --repo owner/repo --path docs        # → Required flag "type" not set
dox add mylib -t github -r owner/repo -p docs      # → flag provided but not defined: -r

# C4 — non-GitHub hosts go to api.github.com
# [sources.gl] repo="gitlab-org/gitlab" path="doc" host="gitlab.com"   → "github API returned status 404 …"

# C6 — off-by-one between search and cat
dox search queryKey --content --json --limit 1      # → line 152
dox cat tanstack-db collections/local-only-collection.md --offset 152 --limit 1   # → line 153's text
```

**P1 timing (dahlia/logtape `docs/`, 35 files):** `dox sync` 5.01 s / 37 REST calls · re-sync 0.16 s / 2 calls · `curl codeload…tar.gz | tar xz --include '*/docs/*.md…'` 0.36 s / 0 calls · `xargs -P 8 curl raw.githubusercontent.com/dahlia/logtape/main/docs/{}` 0.79 s / 0 calls (byte-identical).

**P3–P6 harness:** a `cmd/auditbench` program inside a scratch copy of the module (importing `internal/...`) loaded a `dox.toml` covering the 8-source corpus and measured `manifest.Generate`, `manifest.Load`, `search.Content` and `search.Metadata` with `runtime.MemStats`. A CPU and alloc profile (`runtime/pprof`) gave the P3 breakdown. A parallel-parse prototype (errgroup over files within each source) gave the −36% figure. A prefix-scaling program gave the P4 curve.

## Appendix B — Commands run

```bash
cd /Users/takinprofit/Dev/dox
go version                                   # go1.27.3
go build ./... && go vet ./...               # clean
go test -race -cover ./...                   # pass; coverage per package in §0
go tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint version   # no such tool
golangci-lint@v2.15.0 run ./...              # 40 issues
go fix -diff ./...                           # empty at go 1.25.7; 1 hunk at go 1.27 (scratch copy)
modernize ./...                              # clean
govulncheck ./...                            # 0 reachable; 1 imported-package + 36 module advisories
go list -m -u all                            # outdated direct deps (§G4)
go test -run '^$' -bench . -benchmem ./internal/search ./internal/manifest
```

No tracked files in the dox repo were modified (`git status` clean). Experiments that needed code changes (the `go 1.27` bump, the benchmark harnesses, the parallel-manifest prototype) ran in a scratch copy of the module. Docker-based verification of R10 was not run because the Docker daemon was not running.
