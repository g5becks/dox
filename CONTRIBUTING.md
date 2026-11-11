# Contributing to dox

Use Go 1.26 or newer to build dox. Development uses Go 1.27.3, selected by the
root module's toolchain directive. Keep changes focused, preserve existing tests,
and explain compatibility changes in the changelog with migration steps.

## Setup

```sh
git clone https://github.com/g5becks/dox.git
cd dox
go mod download
go tool -modfile=tools/go.mod lefthook install
go tool -modfile=tools/go.mod task build
./bin/dox --help
```

Task, golangci-lint, Lefthook, GoReleaser, goimports, govulncheck, and benchstat are pinned in
`tools/go.mod`. Invoke them as `go tool -modfile=tools/go.mod <tool>` from the
repository root. Actionlint is pinned separately in `tools/actionlint/go.mod` to preserve its
compatible YAML graph. No global installation is needed. Beads v0.49.6 is isolated in
`tools/beads/go.mod`; always use `scripts/bd`. Its wrapper disables CGO and the
daemon. [The tools guide](tools/README.md) explains the separate graphs.

## Issues and commits

Read [AGENTS.md](AGENTS.md) and the relevant issue before implementing changes.
The audit in `dox-audit.md` explains findings cited by the release issues.

```sh
scripts/bd ready
scripts/bd show <id>
scripts/bd update <id> --claim
scripts/bd update <id> --append-notes 'Plan: ...'
```

Keep one issue in progress. Add the plan, decisions, verification evidence, and
resume instructions to its notes. Mark acceptance checkboxes only after verifying
them. Finish with `scripts/bd close <id>` and `scripts/bd sync`. This beads version
only exports `.beads/issues.jsonl`; stage and commit the export with the code.
Do not make standalone export commits or install a global beads version.

Use Conventional Commits, such as `fix(source): handle an empty response` or
`test(cli): cover missing config`. Compatibility changes use `!`, such as
`fix(config)!: reject unsafe output paths`, and need CHANGELOG Changed migration
instructions. Do not bypass Git hooks or rewrite pushed commits. Push when the
specific task calls for it.

## Local checks and hooks

```sh
go build ./...
go vet ./...
go test -race ./...
go tool -modfile=tools/go.mod task lint
gofmt -l .
go fix -diff ./...
go mod tidy -diff
(cd tools && go mod tidy -diff)
(cd tools/beads && go mod tidy -diff)
(cd tools/actionlint && go mod tidy -diff)
```

Lint must report zero issues. Formatting, modernization, and tidy checks must
produce no diff. Run tidy from each module's own directory: a root-level
`-modfile` invocation scans root packages, not the tools module's packages.

Task targets include `build`, `dev`, `test` (race tests with coverage),
`test:coverage`, `lint`, `lint:fix`, `fmt`, `deps`, `deps:tidy`, and `all`.
List them with `go tool -modfile=tools/go.mod task --list`. To apply supported
lint fixes, run `go tool -modfile=tools/go.mod task lint:fix`, then review the diff.
Fix remaining findings by refactoring; do not lower lint thresholds or disable
linters. Document any confirmed false-positive suppression in the issue notes.

Pre-commit runs goimports on staged Go files, restages formatting fixes, then
lints new findings against HEAD. Commit-msg validates the subject. Pre-push runs
full lint and race tests. To run the push checks manually:

```sh
go tool -modfile=tools/go.mod lefthook run pre-push
```

`bash scripts/ci-local.sh <ref>` is the local gate; require its final
`RESULT: PASS`. It checks clean clones with both supported Go versions, macOS
host jobs, Linux Docker jobs, and Windows amd64 compilation/vet. Logs and result tables
are saved under `/Users/takinprofit/Dev/dox-release/ci-local/<sha>/logs/`.
The local runner requires Python 3, curl, and an already-running Docker daemon.
It detects Colima when needed and never starts or stops it. All native checks are
shared with GitHub in `scripts/ci-check.sh`; tools are built with Go 1.27.3 through `go tool`.
Analyzers and project build/vet/race/fix/tidy checks use the selected matrix Go
version. Native Windows execution remains a GitHub-only check. GitHub Actions currently cannot run because of the account's
billing lock; report local results honestly rather than claiming an Actions run.

## Test strategy

Unit tests live beside implementations. Use table-driven cases, `t.Parallel()`
for independent tests, `t.TempDir()` for writes, and `t.Context()` for test-scoped
operations. Never run destructive CLI repros outside a temporary directory.

Use `httptest` for deterministic HTTP behavior: conditional requests, failures,
cancellation, pagination, and rate limits. The CLI scripts in `cmd/dox/testdata/script/` exercise command output,
exit status, flags, and config files through `testscript.Main`. Run them with
`go test ./cmd/dox -run TestScripts`. Local HTTP, environment expansion,
portable paths, and JSON goldens are documented beside the scripts.
`internal/testutil/fakegithub` provides deterministic GitHub routes and injectable
status, rate-limit, Retry-After, latency, cancellation, and truncation behavior.
From the regression-harness issue onward, register and first demonstrate each
regression failing before fixing it. Keep all cases; never weaken assertions or
add known failures just to pass the suite.

Parser, manifest, and search benchmarks are alongside their packages. The
pinned corpus, `DOX_BENCH_CORPUS`, and v0.3.0 baseline are documented in
[testdata/bench/README.md](testdata/bench/README.md). Compare relevant performance changes with that same corpus and machine
using benchstat; record the commands, baseline, and results in issue notes. Live
GitHub benchmarks require an explicit token in the process environment; never
print credentials or embed them in output or fixtures.

## Structure and extensions

`cmd/dox/` contains CLI commands. `internal/config`, `lockfile`, `source`, `sync`,
`parser`, `manifest`, `search`, and `ui` implement the corresponding core behavior.

For a source, add config/validation, implement `source.Source`, register it in the
factory, and cover its behavior with fake HTTP tests. For a parser, implement
`parser.Parser`, register it in the manifest generator, and add parser and
integration cases. Update README examples and supported features together with
the implementation. Use `samber/oops` error codes, useful context, and actionable
hints consistently with the existing packages.

## Release preparation

Finish all release children and checks before preparing a release. Once the
release tooling is implemented, `scripts/release-prep.sh X.Y.Z` rolls CHANGELOG,
creates a local annotated tag, verifies local CI, builds a GoReleaser **snapshot**,
and runs binary/container smoke tests. Store artifacts outside the source tree
under `/Users/takinprofit/Dev/dox-release/` in this development environment.

Use a release branch and a merge commit PR; do not squash, rebase, or use admin
merge. Preserve the local tag as an ancestor of main. Tag publication, production
GoReleaser, image pushes, and GitHub release publication are the maintainer's
separate responsibility. Agents must never push release tags.

Contributions are licensed under the [MIT license](LICENSE).

## Container HTTPS checks

`bash scripts/container-smoke.sh` builds the same digest-pinned nonroot image as
GoReleaser, syncs the public `https://hono.dev/llms.txt` source over verified
HTTPS, and requires a nonempty result. It may also take an absolute path to an
already-built Linux binary for the Docker daemon's architecture. No image is
pushed. The smoke runs for each supported Go version in ci-local and the Ubuntu
workflow legs. On macOS it creates owned temporary fixtures under
`DOX_RELEASE_ROOT` (default `/Users/takinprofit/Dev/dox-release`) so Colima can
share the bind mount; other platforms use the normal temporary directory.
The container uses the bind mount owner's UID/GID during the test, and its
configured default nonroot user is checked separately.
