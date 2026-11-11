# Working on dox

Use shell commands in the sandbox: `rg` for search, `go doc`/`go list` for APIs,
and Go tools for checks. MCP tools require unavailable approval in this run;
a rejected MCP call is a tool limitation, so use the shell equivalent. Sandbox file denials, missing credentials, and interactive prompts are hard stops.
Temporary network/DNS/connection/timeout errors and HTTP 429/5xx responses are
retryable: retry the same command up to three more times after sleeps of 120,
300, and 600 seconds. Record every attempt and its outcome in issue notes; a
retried success does not erase previous failures. Do not switch proxies, hosts,
or credentials. If exhausted, mark the issue `BLOCKED: network` and continue
ready work that needs no network. Do not work around sandbox restrictions.

## Issue tracking

Use **only `scripts/bd`**, the project-pinned beads v0.49.6 wrapper. It uses
`tools/beads/go.mod`, disables CGO, and runs without a daemon. Never install,
upgrade, or invoke a global `bd`. Never run `bd init`, install beads hooks,
use `sync --full`, or edit `.beads/` files by hand. Ignore the harmless
`beads.role not configured` warning. The `bv` TUI is not part of this workflow.

```sh
scripts/bd list --status in_progress
scripts/bd ready
scripts/bd show <id>
scripts/bd update <id> --claim
scripts/bd update <id> --append-notes 'Plan: ...'
scripts/bd update <id> --acceptance '<existing criteria, verified items ticked>'
scripts/bd close <id> --reason 'Completed and verified ...'
scripts/bd sync
```

Have one issue in progress at a time. Finish existing work before claiming the
lowest-numbered ready child of the current release epic. Read its dependencies,
acceptance criteria, and the cited findings in `dox-audit.md`. Record the plan,
implementation decisions, behavior changes, and actual verification results in
issue notes. Tick each acceptance item only after verifying it; close an issue
only when all items are checked. Create out-of-scope follow-ups under the current
epic, depending on the originating issue.

In beads v0.49.6, `scripts/bd sync` **only exports** `.beads/issues.jsonl`.
Commit that export **with the implementation**, never as a standalone sync commit.
If work stops mid-issue, leave changes in the working tree and describe how to
resume in the issue notes. Do not use `git stash`.

## Tools and checks

The CLI module requires Go 1.26; development uses Go 1.27.3. Development
tools are pinned in `tools/go.mod`; beads has a separate compatible dependency
graph in `tools/beads/`. See [tools/README.md](tools/README.md).

```sh
go tool -modfile=tools/go.mod task lint
go tool -modfile=tools/go.mod lefthook install
go build ./...
go vet ./...
go test -race ./...
gofmt -l .
go fix -diff ./...
go mod tidy -diff
(cd tools && go mod tidy -diff)
(cd tools/beads && go mod tidy -diff)
(cd tools/actionlint && go mod tidy -diff)
```

Lint must report zero issues; formatting, fix, and tidy checks must produce no
diff. Refactor findings instead of disabling linters, lowering thresholds, or
silencing real problems. A false-positive suppression needs a specific inline
reason and an entry in issue notes. Keep regression cases and existing tests.
From the regression-harness issue onward, demonstrate each fix with a previously
failing registered case. Never add known-failure entries to make a suite pass.

Pre-commit formats/restages staged Go files and checks new lint findings against
HEAD. Pre-push runs full lint and race tests; commit-msg validates Conventional
Commits. Do not bypass hooks. Document compatibility changes in CHANGELOG Changed,
including migration steps, and use a `!` commit subject.

## Release work

Release branches are `release/v0.4.0` and then `release/v0.5.0`, with the latter
created from updated main after the first release prep merges. Never work on
`dox-5il`: it belongs to the maintainer. Do not modify other repositories.

For committed refs, `bash scripts/ci-local.sh <ref>` must print
`RESULT: PASS`; it replaces unavailable GitHub Actions execution during the
billing lock. Record the checked SHA and results. Windows is cross-compiled and
vetted locally; native Windows runs belong to GitHub. Do not claim an Actions
run occurred. See CONTRIBUTING for the validation strategy and local runner.

Commit implementation and its beads export together using Conventional Commits.
Push only when the task explicitly authorizes it, and only to the authorized
release branches or main for release merges and their follow-up export commits.
Never force-push, amend/rebase pushed commits, delete remote refs, or use an admin
merge. Release-prep tags stay **local**. Never push tags, create GitHub releases,
run a non-snapshot GoReleaser release, or push container images. Publication
belongs to the maintainer.
