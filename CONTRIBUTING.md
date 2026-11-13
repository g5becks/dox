# Contributing to dox

Use Go 1.26 or newer to build dox. Development uses Go 1.27.4, selected by the
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
(cd tools/buildx && go mod tidy -diff)
```

Lint must report zero issues. Formatting, modernization, and tidy checks must
produce no diff. Run tidy from each module's own directory: a root-level
`-modfile` invocation scans root packages, not the tools module's packages.

Task targets include `build`, `dev`, `test` (race tests with coverage),
`test:coverage`, `fuzz`, `lint`, `lint:fix`, `fmt`, `deps`, `deps:tidy`, and `all`.
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
shared with GitHub in `scripts/ci-check.sh`; tools are built with Go 1.27.4 through `go tool`.
Analyzers and project build/vet/race/fix/tidy checks use the selected matrix Go
version. Native Windows execution remains a GitHub-only check. GitHub Actions currently cannot run because of the account's
billing lock; report local results honestly rather than claiming an Actions run.

Local snapshot image builds use the pinned Buildx dispatcher in
`scripts/with-buildx.sh`; see [tools/README.md](tools/README.md) for invocation
and temporary artifact-directory requirements.

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

### Coverage floors

`scripts/ci-check.sh` (GitHub and ci-local) runs `scripts/coverage-floors.sh`
on the race-test cover profile and fails when a package drops below its floor in
`scripts/coverage-floors.txt`. The check is strict in both directions: a new
package must register a floor (0 is allowed for untested helpers), and a floor
for a removed or renamed package must be deleted. `cmd/dox`, `internal/sync`
and `internal/source` use the thresholds set by dox-4e1.9 (70, 80, 85); every
other package sits at the integer floor of its coverage at v0.5.0. Raise a floor
when coverage improves for good; lower one only with a reason in the commit.
Floors are checked on Linux and macOS only, because Windows has platform-specific
files and skipped tests and cannot be measured locally. Inspect a profile with:

```sh
go test -race -coverprofile=coverage.out ./... && bash scripts/coverage-floors.sh coverage.out
```

### Fuzzing

Fuzz targets cover the outline scanner, config loading and path validation,
include/exclude globs, `normalizeRepoPath`, and TOML source-section replacement.
Their seed corpora run as ordinary tests in `go test ./...`. CI also fuzzes each
target for 10 seconds (`scripts/fuzz.sh 10s`, Linux and macOS). For a long local
run use `go tool -modfile=tools/go.mod task fuzz` (5 minutes per target) or
`task fuzz FUZZTIME=30m`. `scripts/fuzz.sh` registers targets explicitly and
fails if a `Fuzz*` function is not listed, so add new targets there. A failing
input is saved under `testdata/fuzz/`; commit it as a regression seed with the
fix.

### Concurrency

`internal/sync` runs more sources than workers under `-race` and asserts that
the lock, the files on disk and the result counters agree
(`TestParallelSyncKeepsEveryLockEntry`). Keep new shared state in `sync.Run`
covered by that test.

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

## Release packaging

Run the pinned `go tool -modfile=tools/go.mod goreleaser check` without bypass
flags. Snapshot images use `scripts/with-buildx.sh` and an owned temporary dist
path, as described in tools/README.md. The shared Dockerfile expects binaries
under `linux/amd64/dox` or `linux/arm64/dox` in its build context. BuildKit supplies
TARGETPLATFORM; a legacy Docker builder needs the matching `--build-arg`.

GoReleaser generates a Homebrew cask under `Casks/dox.rb` (macOS universal and
Linux amd64/arm64). Its declarative post-install step runs `dox --version`,
preserving the former formula's executable probe. The maintainer publishes it to the existing tap; this repo's
snapshot checks do not modify that tap. Existing formula users must uninstall
the formula and install the cask, as shown in README. Docker v2 publishes the
main version/latest multi-platform tags and keeps the per-architecture aliases;
snapshots build/load only architecture-suffixed local images without publishing.

## Preparing a release locally

The release-prep issue first verifies that every other epic child is closed;
v0.4.0 also requires an empty `knownFailing` registry. Plan approval does not
waive those release prerequisites. GitHub Actions is billing-locked, so use the
macOS local runner with an already-running Docker daemon. Do not start Colima.

On a clean `release/vX.Y.Z` branch with nonempty `[Unreleased]` entries:

```sh
bash scripts/release-prep.sh X.Y.Z --dry-run
bash scripts/release-prep.sh X.Y.Z
```

Dry-run only reads preconditions; it changes no files or refs. Preflight rejects
a wrong/detached branch, dirty index/tree, an empty changelog section, an existing
local/origin tag, or an existing destination. The real run rolls the changelog,
commits it and creates an annotated **local** tag. It then requires ci-local PASS
on the tag, strict GoReleaser check and a complete snapshot through pinned Buildx.
It verifies snapshot checksums, records every binary size, runs the native macOS
arm64 binary's `--version` and URL sync, runs all Linux binaries (amd64, arm64,
arm/v7) in digest-pinned containers, and checks container HTTPS. Foreign binaries
use user-mode QEMU in a pinned, nonroot container; native binaries use the pinned
CA base. This requires no privileged container, global emulator installation or
VM/binfmt changes. The product image and HTTPS checks still use the CA base.

Evidence lives in `/Users/takinprofit/Dev/dox-release/vX.Y.Z/`: `ci-tag.log`,
`artifacts/`, `binary-sizes.json`, `binaries.tsv`, snapshot/config-check logs and
native/container smoke logs. `DOX_RELEASE_ROOT` can select an owned temporary
parent for integration tests. Destinations are created once and never reused or
cleaned by a subsequent run. Snapshot versions have the configured `-next`
suffix; they are verification artifacts, not published release binaries.

The script never pushes refs or publishes: it prints maintainer commands only.
After success, run ci-local on the release branch, open a PR with the changelog
section and issue IDs, and attach both tag/branch tables. Merge with `--merge`
without `--admin`; update main and confirm the local tag commit is its ancestor.
The maintainer alone pushes the tag and runs a non-snapshot release. Pushing a Go
tag can publish the module through the Go proxy. Preserve local tags throughout.

A failed check stops preparation and preserves logs, commit and tag for review.
Do not merge, repoint/delete tags, or rerun the whole script over that evidence.
Mark the release-prep issue blocked with the failing step/log. Temporary network
failures follow the documented 120/300/600-second same-command retry policy;
record each attempt, and retry the failing step rather than erase its evidence.
`bash scripts/test-release-prep.sh` checks preflight/dry-run behavior using owned
local repositories. Full preparation is tested separately in an owned temporary
clone with a local bare origin; that mechanics test does not prepare the actual
release or waive its open-issue/known-failure prerequisites.

Before actual release preparation, `bash scripts/test-release-prep-full.sh X.Y.Z`
exercises the real script against committed HEAD in a new temporary clone and
local bare origin. It installs fixture hooks, guards git/image pushes and
non-snapshot GoReleaser execution, verifies dry-run immutability and unchanged
origin/source refs, and preserves its full CI/snapshot/smoke evidence. This
expensive test is manual to avoid recursively invoking CI inside every CI job.
