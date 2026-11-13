# dox

CLI tool that syncs, searches, and reads library docs locally.

## Install

### Homebrew (macOS/Linux)

```bash
brew install --cask g5becks/tap/dox
```

The v0.4.0 tap package is a cask on macOS and Linux. If the older formula is
installed, run `brew uninstall --formula dox` before the cask install above.
Update Homebrew first (`brew update`) for Linux cask support.

### Download Binary

Download pre-built binaries from the [releases page](https://github.com/g5becks/dox/releases).

**Linux/macOS:**
```bash
curl -L https://github.com/g5becks/dox/releases/latest/download/dox_Linux_x86_64.tar.gz | tar xz
sudo mv dox /usr/local/bin/
```

**Windows:**
Download the `.zip` from releases, extract, and add to PATH.

### Linux Packages

```bash
# Debian/Ubuntu
wget https://github.com/g5becks/dox/releases/latest/download/dox_linux_amd64.deb
sudo dpkg -i dox_linux_amd64.deb

# RedHat/Fedora/CentOS
wget https://github.com/g5becks/dox/releases/latest/download/dox_linux_amd64.rpm
sudo rpm -i dox_linux_amd64.rpm

# Alpine
wget https://github.com/g5becks/dox/releases/latest/download/dox_linux_amd64.apk
sudo apk add --allow-untrusted dox_linux_amd64.apk

# Arch
wget https://github.com/g5becks/dox/releases/latest/download/dox_linux_amd64.pkg.tar.zst
sudo pacman -U dox_linux_amd64.pkg.tar.zst
```

### Docker

```bash
docker pull ghcr.io/g5becks/dox:latest
docker run --rm ghcr.io/g5becks/dox:latest --version
```

The image includes CA certificates and defaults to a nonroot user (UID 65532).
For a local project bind mount, use your own UID/GID so downloaded files remain
writable by your account (macOS/Linux):

```bash
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/workspace" -w /workspace ghcr.io/g5becks/dox:latest sync
```

Existing setups that relied on root-owned bind-mount writes should switch to
that explicit user or grant the image user access to the mounted directory.


### Go Install

```bash
go install github.com/g5becks/dox/cmd/dox@latest
```

### Build from Source

Requires Go 1.26 or newer. Development uses the pinned Go 1.27.4 toolchain.

```bash
git clone https://github.com/g5becks/dox.git
cd dox
task build
./bin/dox --help
```

## Quick Start

1. Create a starter config:

<!-- doxtest: workspace=bare -->

```bash
dox init
```

2. Edit `dox.toml` and add sources.

3. Sync docs:

<!-- doxtest: workspace=synced -->

```bash
dox sync
```

4. Add `.dox/` to `.gitignore`.

Manifest updates reuse unchanged entries and parse changed files in parallel.
The manifest uses compact JSON; `generated` reflects input timestamps, and
`last_sync` uses the lock timestamp or file mtime. An unchanged snapshot is not
rewritten. Existing manifests regenerate automatically when cache metadata or
parser semantics change.

## Config

<!-- doxtest: config -->

```toml
# dox.toml
output = ".dox"

# Default parallelism for syncing (default: 4x CPU cores, min 10, max 16)
# Override per-command with: dox sync --parallel N
max_parallel = 20

# Per-source sync and HTTP request timeout (default: "30s")
# Override per-command with: dox sync --timeout 30s
timeout = "30s"

# Global exclude patterns applied to all git sources
# Per-source excludes add to (not replace) these global patterns
excludes = [
    "**/*.png",
    "**/*.jpg",
    "node_modules/**",
    ".github/**",
]

# GitHub source — type inferred from 'repo'
[sources.goreleaser]
repo = "goreleaser/goreleaser"
path = "www/docs"
patterns = ["**/*.md"]  # Override default patterns
exclude = ["**/changelog.md"]  # Adds to global excludes

# URL source — type inferred from 'url'
[sources.hono]
url = "https://hono.dev/llms-full.txt"
```

## Commands

The global `--config path/to/dox.toml` (`-c`) flag overrides config discovery for
every command, including `init`, which creates the file at that path. Set
`DOX_CONFIG` to make it the default (the flag wins). The global flags `--json`,
`--verbose` and `--allow-external-output` go before the command (for example
`dox --json list`) or after it.

Output commands (`list`, `collections`, `files`, `cat`, `outline`, `search`,
`doctor`) share the same options. `--format table|json|jsonl|csv` selects the
output format and `--json` stays as an alias for `--format json`. `--limit N`
caps the rows, and `--limit 0` means no limit. Without `--format`, `collections`,
`files`, `cat`, `outline` and `search` use `display.format` from the config file
(default `table`). `jsonl` writes one JSON object per line, and `csv` writes a
header row followed by one row per item.

### sync

<!-- doxtest: workspace=synced -->

```bash
dox sync                    # Sync all sources
dox sync goreleaser hono    # Sync specific sources
dox sync --force            # Force re-download
dox sync --clean            # Clear owned output before syncing
dox sync --dry-run          # Preview without downloading
dox sync --parallel 5       # Override parallelism
dox sync --timeout 15s      # Override per-source timeout
dox sync --check            # Exit 3 if any source is stale; writes nothing
dox sync --locked           # Reproduce the lock exactly (for CI)
```

Concurrent `dox sync` and `dox clean` runs on the same output directory are
serialized with an advisory lock (`.dox-run.lock`): a second run waits up to
`--lock-timeout` (default `30s`) and then fails with exit code 1. Dry runs and
`--check` do not take the lock.

`--check` compares each source with upstream without writing anything: it
exits 0 when everything is current and 3 when a source would change
(`dox --json sync --check` prints `{"stale": [...], "current": [...]}`).
GitHub sources compare folder tree SHAs; URL sources use `ETag`/`Last-Modified`
or, without them, the content digest recorded in the lock.

`--locked` reproduces `.dox/.dox.lock`: GitHub sources fetch the locked commits
instead of resolving `ref`, and URL sources must match their recorded content
digest (URLs cannot be pinned, only verified). It fails with exit code 2 when
the config no longer matches the lock: a changed source definition, a source
missing from the lock, or a locked source missing from the config. Commit the
lock file to make syncs reproducible across machines.

### list

<!-- doxtest: workspace=synced -->

```bash
dox list                    # Show configured sources
dox list --verbose          # Show full details
dox list --json             # JSON output
dox list --format csv       # CSV output (jsonl is also available)
dox list --files            # Show synced files
```

### add

<!-- doxtest: workspace=empty -->

```bash
# GitHub source (type inferred from --repo)
dox add goreleaser --repo goreleaser/goreleaser --path www/docs

# URL source (type inferred from --url)
dox add hono --url https://hono.dev/llms-full.txt

# Short aliases: -r/--repo, -p/--path, -u/--url, -R/--ref
dox add my-lib -r owner/repo -p docs -R v1.2.3

# Overwrite existing source
dox add goreleaser --repo goreleaser/goreleaser --path www/docs --force
```

`--type` is optional (inferred from `--repo` or `--url`). `--host` accepts only
`github.com` until other providers land in v0.5.0. Source names may contain
letters, digits, `-` and `_`. `dox add` writes the config atomically and keeps its
file permissions. `--force` only rewrites a section in the simple form `dox add`
itself produces (single-line `key = value` pairs, no comments inside). Otherwise it
refuses with exit code 2 and leaves the file unchanged, so edit that section by hand.

### Output safety

`output` must be a relative directory below the directory containing `dox.toml`.
Use `.dox` or `docs/generated`; migrate `output = "."`, `".."`, and absolute
paths to a dedicated relative directory. Source `out` and URL `filename` paths
must stay inside that output. Symlinks cannot redirect writes outside it.

For an intentional separate external directory, opt in on each invocation:

<!-- doxtest: workspace=synced -->

```sh
dox --allow-external-output sync
dox --allow-external-output clean
```

The config directory, its ancestors, and the filesystem root remain protected,
including aliases through symlinks. The first non-dry sync writes `.dox-root`.
`clean` and `sync --clean` require that marker or an existing `.dox.lock`; they
refuse unowned directories. Migrate existing output by running a normal
`dox sync` first. Whole-output cleanup leaves the output directory and its
ownership marker in place. Selected-source cleanup removes only those sources.

### clean

<!-- doxtest: workspace=synced -->

```bash
dox clean                   # Remove all synced docs
dox clean goreleaser        # Remove specific source
```

### init

<!-- doxtest: workspace=bare -->

```bash
dox init                    # Create starter dox.toml
dox init -c docs/dox.toml   # Create it at another path (or set DOX_CONFIG)
```

### collections

<!-- doxtest: workspace=synced -->

```bash
dox collections             # Table output
dox collections --json      # JSON output
dox collections --format csv  # CSV output (jsonl is also available)
dox collections --limit 5   # Limit results
```

An unknown collection name in `files`, `cat`, `outline` or `search --collection`
exits with code 4 and a `hint:` that suggests close matches (`Did you mean
"goreleaser"?`).

### files

<!-- doxtest: workspace=synced -->

```bash
dox files goreleaser                       # Default table output
dox files goreleaser --json                # JSON output
dox files goreleaser --format csv          # CSV output
dox files goreleaser --fields path,type,size  # Custom fields
dox files goreleaser --limit 10            # Limit results
dox files goreleaser --limit 0             # Show all files (no limit)
dox files goreleaser --desc-length 100     # Shorter descriptions
```

Available fields: `path`, `type`, `lines`, `size`, `description`, `modified`.
`--all` is deprecated (it warns and means `--limit 0`); it is removed in v0.6.0.

### cat

<!-- doxtest: workspace=synced -->

```bash
dox cat goreleaser docs/install.md                      # Show entire file
dox cat goreleaser docs/install.md --line 10            # Start at line 10
dox cat goreleaser docs/install.md --limit 20           # Show 20 lines
dox cat goreleaser docs/install.md:11 --limit 20       # Range
dox cat goreleaser docs/install.md --no-line-numbers    # No line numbers
dox cat goreleaser docs/install.md --json               # JSON output with metadata
dox cat goreleaser docs/install.md --format jsonl       # One {"line","text"} object per line
dox cat goreleaser docs/install.md --format csv         # line,text rows
```

Line locations in search results, outlines, `cat --line N`, and `file:N` use
1-based numbering. For example, `dox cat goreleaser docs/install.md:10 --limit 1`
reads the line numbered 10. JSON adds `line` for that starting line and retains
the legacy 0-based `offset` field. Use only one of `--line`, `file:N`, or
`--offset`. The old `--offset N` still works with a deprecation warning; migrate
to `--line N+1` before its removal in v0.6.0.

### outline

Show document structure — headings for markdown/MDX, exports for TypeScript:

<!-- doxtest: workspace=synced -->

```bash
dox outline goreleaser docs/install.md
dox outline goreleaser docs/install.md --json
dox outline goreleaser docs/install.md --format csv     # kind,line,level,type,text rows
```

Markdown/MDX outlines use source line positions, including frontmatter and MDX
imports, and recognize both ATX and setext headings. Re-run `dox sync` after
upgrading to regenerate manifests with corrected line numbers and metadata.
YAML/TOML metadata blocks decode up to 64 KiB; formatted heading/description
text has a 256 KiB per-file budget, after which normalized source text is kept.

### search

Search across documentation metadata or file contents.

**Metadata search** (default) — fuzzy matches across file paths, descriptions, headings, and exports:

<!-- doxtest: workspace=synced -->

```bash
dox search "installation"                    # Search all collections
dox search "hooks" --collection react        # Search specific collection
dox search "config" --limit 10               # Limit results
dox search "api" --json                      # JSON output
dox search "guide" --format csv              # CSV output
dox search "guide" --desc-length 80          # Truncate descriptions
```

**Content search** — literal or regex patterns within file contents:

<!-- doxtest: workspace=synced -->

```bash
dox search "useState" --content              # Literal (case-insensitive)
dox search "configuration" --content --collection goreleaser
dox search "func.*Logger" --content --regex  # Regex
dox search "import" --content --limit 20
```

Content search skips binary files and files over 50MB. Regex requires `--content`, and an invalid pattern exits with code 2.

**Typical agent workflow:**

<!-- doxtest: workspace=synced -->

```bash
dox sync                                    # Ensure docs are current
dox collections --json                      # Discover available docs
dox search "authentication" --json          # Find relevant topics
dox files react --json --limit 100          # Browse collection files
dox cat react docs/hooks.md                 # Read specific content
```

### doctor

Diagnose a setup without changing it: config discovery and validity, per-host
token presence (the value is never printed), the github.com API rate limit,
output-directory safety, and consistency between the config, the lock and the
files on disk. Each check is `ok`, `warn` or `fail`; any `fail` exits with code 1.

<!-- doxtest: workspace=synced -->

```bash
dox doctor --offline        # Skip the checks that need the network
dox doctor --offline --json # Machine-readable report
```

<!-- doxtest: skip (queries api.github.com, which tests must not reach) -->

```bash
dox doctor                  # Also report the github.com API rate limit
```

### completion

Print a shell completion script for `bash`, `zsh`, `fish` or `pwsh`:

<!-- doxtest: workspace=bare -->

```bash
dox completion bash
dox completion zsh
```

For example, `source <(dox completion bash)` in `~/.bashrc`, or
`dox completion fish > ~/.config/fish/completions/dox.fish`.

### mcp

Serve the synced documentation to an AI agent over the [Model Context
Protocol](https://modelcontextprotocol.io) (stdio). Point your MCP client at the
command and the agent can browse your docs without shelling out or loading whole
files into its context.

<!-- doxtest: skip (speaks JSON-RPC on stdin and stdout; the mcp script test drives it) -->

```bash
dox mcp                       # Run from the project directory
dox --config path/to/dox.toml mcp
```

The server exposes five **read-only** tools that mirror the CLI: `collections`,
`files`, `outline`, `cat` and `search`. It never syncs, writes or touches the
network; run `dox sync` yourself and the next call sees the new content. Every
list is capped and paged so one call cannot flood the agent's context: `cat`
returns 500 lines by default (2000 at most) with a `next_line` to continue,
`search` 20 results (100), and `files` 200 entries (1000) with `next_offset`.
Failures come back as tool errors with the same hint the CLI prints, including
"did you mean" suggestions.

Most clients take the same entry, for example in Claude Desktop's
`claude_desktop_config.json`, Cursor's `mcp.json` or VS Code's `.vscode/mcp.json`:

```json
{
  "mcpServers": {
    "dox": { "command": "dox", "args": ["mcp"] }
  }
}
```

If the client does not start in your project directory, pass the config:
`"args": ["--config", "/path/to/dox.toml", "mcp"]`. Nothing but protocol is
written to stdout; a hint goes to stderr only when you run `dox mcp` in a
terminal. Requests piped in (`printf '...' | dox mcp`) are all answered before
the server exits at the end of input.

### version

`dox version` (or `dox --version`) prints the version, commit and build time.
Release binaries carry these values; a binary built with `go install
github.com/g5becks/dox/cmd/dox@vX.Y.Z` reads them from the Go build info.

### Error output and exit codes

Errors print their message and an actionable `hint:` line to `stderr` when
available. Passing `--verbose` appends context `fields:` and the unwrapped
`cause:` chain. Passing `--json` on any command emits a structured error
envelope to `stderr`:

```json
{
  "error": {
    "code": "not_found",
    "message": "source \"nope\" not found in config",
    "hint": "Run 'dox list' to see configured sources",
    "fields": {
      "legacy_code": "SOURCE_NOT_FOUND",
      "source": "nope"
    }
  }
}
```

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | runtime/network |
| 2 | usage/config |
| 3 | partial sync failure |
| 4 | not found |
| 130 | interrupted |

## Go API

The `github.com/g5becks/dox/dox` package embeds dox in a Go program: load a
project, sync it, and read or search the result, with the same behavior and the
same typed errors as the CLI. It needs Go 1.26 or newer.

```go
project, err := dox.Load("") // "" discovers dox.toml upward from the working directory
if err != nil {
	return err
}
if _, err := project.Sync(ctx, dox.SyncOptions{}); err != nil {
	return err
}
for result, err := range project.Search(ctx, dox.Query{Text: "retry", Content: true, Limit: 5}) {
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s:%d %s\n", result.Collection, result.Path, result.Line, result.Text)
}
```

`Project` offers `Sync`, `Collections`, `Files`, `Outline`, `Cat` and `Search`.
Every method takes a `context.Context` and returns a `*dox.Error` carrying a
stable `Code` (`CodeUsage`, `CodeNotFound`, `CodeNetwork`, ...) and a `Hint`;
use `errors.Is` with `dox.ErrCollectionNotFound`, `dox.ErrFileNotFound`,
`dox.ErrSourceNotFound` or `dox.ErrNotSynced` to branch on a cause. A `Project`
keeps no mutable state, so it is safe to share between goroutines, and syncs of
one output directory are serialized by the same advisory lock the CLI uses. An
output directory outside the project is refused unless you pass
`dox.WithExternalOutput()`, matching `--allow-external-output`.

The API is new in v0.5.0 and may change before v1.0.0; incompatible changes are
listed under Changed in the [CHANGELOG](CHANGELOG.md). See the package
documentation (`go doc github.com/g5becks/dox/dox`) for every type.

### Dependency decision

`dox mcp` is built on the official
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
v1.8.0 (MIT, moving to Apache-2.0) rather than a hand-written JSON-RPC subset,
because the protocol (initialization, schema-validated tools, cancellation) is
easy to get subtly wrong and the SDK tracks the specification. It meets the
Go 1.26 floor (it declares Go 1.25) and `govulncheck` reports nothing
reachable. The cost is binary size: the stripped release binary grows from
11.9 MB to 13.9 MB (+2.0 MB, +17%), from the SDK, its JSON Schema and JSON
packages, and the standard-library code they link (`log/slog`, `encoding/gob`,
`os/exec`). The new modules are `jsonschema-go`, `segmentio/asm`,
`segmentio/encoding`, `yosida95/uritemplate`, `golang.org/x/oauth2` and
`golang.org/x/time`; only the `dox mcp` server imports them.

## Config Reference

`dox.toml` is decoded strictly: unknown keys and wrongly typed values fail with
the key path and line (exit code 2).

### Global

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `output` | string | `.dox` | Output directory root |
| `timeout` | string | `30s` | Per-source sync and HTTP request timeout (`time.ParseDuration`, > 0) |
| `github_token` | string | `$GITHUB_TOKEN` or `$GH_TOKEN` | github.com API token for private repos and higher rate limits |
| `tokens` | table | `{}` | Per-host tokens, for example `tokens = { "github.mycorp.com" = "..." }` |
| `max_parallel` | int | `4 × CPU cores` (min 10, max 16) | Max concurrent source syncs (1–100) |
| `max_per_host` | int | `16` | Max concurrent HTTP requests to one host across all sources (1–64) |
| `download_concurrency` | int | `8` | Max concurrent file downloads within one GitHub source (1–64) |
| `excludes` | []string | `[]` | Global exclude patterns applied to all git sources |

### Git Sources

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | Inferred from `repo` and `host` | `github` (github.com or GitHub Enterprise), `gitlab`, or `gitea` (Gitea, Forgejo, Codeberg; alias `codeberg`) |
| `repo` | Yes | — | Repository in `owner/repo` format (GitLab: `group/subgroup/project` allowed) |
| `host` | No | `github.com` (`gitlab.com` for `gitlab`, `codeberg.org` for `gitea`) | Git hosting domain |
| `path` | Yes | — | Path to directory or file in repo |
| `ref` | No | Default branch (`HEAD`) | Branch, tag, or commit SHA; pinned to a commit at each sync |
| `patterns` | No | `["**/*.md", "**/*.mdx", "**/*.txt"]` | Glob patterns for files to include |
| `exclude` | No | `[]` | Exclude patterns (merged with global `excludes`) |
| `out` | No | Source name | Custom output subdirectory |

Must have either `repo` or `url`, not both.

**Examples:**

```toml
# GitHub (default host)
[sources.docs]
repo = "owner/repo"
path = "docs"
```

**GitHub Enterprise Server:** set `host` to your instance. Hosts whose name
contains `github` infer `type = "github"`; for any other host name, set it
explicitly:

<!-- doxtest: config -->

```toml
[sources.internal-docs]
type = "github"
host = "git.mycorp.com"
repo = "platform/handbook"
path = "docs"
```

dox calls `https://<host>/api/v3` and fetches content and archives through that
API (GHE raw web URLs require a browser session). Tokens come from
`tokens.<host>` in `dox.toml`, then `GH_ENTERPRISE_TOKEN`.

**GitLab (gitlab.com and self-managed):** hosts containing `gitlab` infer
`type = "gitlab"`; otherwise set it. Subgroups are part of `repo`:

<!-- doxtest: config -->

```toml
[sources.glab]
repo = "gitlab-org/cli"
host = "gitlab.com"
path = "docs"

[sources.handbook]
type = "gitlab"
host = "git.example.com"
repo = "platform/docs/handbook"
path = "content"
```

dox calls `https://<host>/api/v4`: one call pins the ref to a commit, one
listing of the parent directory detects an unchanged folder, and changed
folders are listed (paginated) and downloaded as raw files or a single
folder archive. GitLab counts every API request, including raw files, toward
its rate limit. Tokens come from `tokens.<host>`, then `GITLAB_TOKEN`, and are
sent as `PRIVATE-TOKEN`.

**Gitea, Forgejo and Codeberg:** hosts containing `codeberg`, `gitea` or
`forgejo` infer `type = "gitea"`; for other self-hosted instances set it
(`"codeberg"` is accepted as an alias):

<!-- doxtest: config -->

```toml
[sources.codeberg-docs]
repo = "Codeberg/Documentation"
host = "codeberg.org"
path = "content"

[sources.wiki]
type = "gitea"
host = "git.example.org"
repo = "team/wiki"
path = "pages"
```

dox calls `https://<host>/api/v1`: one call pins the ref, a non-recursive tree
listing per path component finds the folder's tree (2 calls for a top-level
folder), and changed folders are listed (paginated) and downloaded as raw files
or one repository archive. Tokens come from `tokens.<host>`, then
`GITEA_TOKEN`, sent as `Authorization: token …`. Gitea reports no stable tree
id for the repository root, so a root-path source compares a digest of its
listing instead.

Any other host without an explicit `type`, and `type = "git"`, fail
validation with "not supported yet" (exit code 2) rather than guessing a
provider.

### URL Sources

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | `url` (inferred) | Set automatically when `url` is present |
| `url` | Yes | — | Direct HTTP/HTTPS URL to a file |
| `filename` | No | Basename from URL | Custom filename for downloaded file |
| `out` | No | Source name | Custom output subdirectory |

### Display

Customize query output in `dox.toml`:

```toml
[display]
default_limit = 50            # Default result limit for files and search
description_length = 200      # Max description/text length in table output
line_numbers = true           # Show line numbers in cat
format = "table"              # Default format: table, json, jsonl, csv
list_fields = ["path", "type", "lines", "size", "description"]
```

## How GitHub sync works

Each GitHub directory source costs 2 REST API calls per sync:

1. `ref` (or `HEAD`) is resolved to a commit SHA, so one sync reads one
   consistent snapshot. The lock records both `ref_resolved` and `commit`.
2. The configured folder's tree is fetched at that commit. When the folder's
   tree SHA matches the lock, the source is up to date, even if other parts of
   the repository changed.

Changed files are downloaded from commit-pinned `raw.githubusercontent.com`
URLs (`download_concurrency` at a time; these do not use API quota), or from a
single commit tarball when 64 or more files changed. Very large trees that
GitHub truncates are listed directory by directory. Single-file sources use 3
API calls. With the unauthenticated limit of 60 calls per hour, about 30
GitHub folder sources sync per hour without a token.

URL sources use `ETag`/`Last-Modified` validators; `--dry-run` sends a `HEAD`
request instead of downloading.

## Output Layout

Default output root is `.dox/`:

```text
.dox/
  .dox.lock
  goreleaser/
  hono/
```

Each source writes into its own directory (source name by default, or `out` override).

## Global Excludes

```toml
excludes = [
    "**/*.png",
    "**/*.jpg",
    "node_modules/**",
    ".vitepress/**",
]
```

- Global excludes apply to all git sources
- Global excludes do **not** apply to URL sources
- Per-source `exclude` patterns add to (not replace) global excludes
- Duplicate patterns are automatically removed
- `dox init` generates a config with comprehensive defaults

## Notes

- Config discovery searches for `dox.toml` or `.dox.toml` from CWD up to filesystem root.
- Relative config paths resolve from the config file directory.
- Token resolution: `tokens.<host>` in config first; for github.com then
  `github_token` → `GITHUB_TOKEN` → `GH_TOKEN`; for GitHub Enterprise hosts then
  `GH_ENTERPRISE_TOKEN`; for GitLab then `GITLAB_TOKEN`; for Gitea then
  `GITEA_TOKEN`.
  A token is needed for private repositories and for many GitHub sources per hour.
- Parallelism uses an explicit `--parallel` value first, then config `max_parallel`, then `min(max(4 × CPU cores, 10), 16)`. An omitted flag (or `--parallel 0`) uses the fallback; explicit positive flag/config values can exceed 16. Pass `--parallel 3` to keep the previous accidental limit.

## Upgrading to v0.4.0

v0.4.0 changes some behavior on purpose. Check each item that applies to you:

- **Output paths**: `output` must be a relative directory below `dox.toml`
  (`.dox` by default). `"."`, `".."` and absolute paths fail with exit code 2;
  move them to a dedicated relative directory, or pass
  `--allow-external-output` for an intentional external directory. `clean` and
  `sync --clean` only touch directories dox owns (`.dox-root` or `.dox.lock`);
  run one normal `dox sync` first to mark existing output.
- **Hosts**: only github.com repositories and URL sources are supported.
  `type = "gitlab"`, `"codeberg"`, `"git"` or any other `host` now fail with
  "not supported yet" (exit code 2) instead of silently querying github.com.
  Use a `url` source for single files until v0.5.0.
- **Parallelism**: the order is `--parallel` flag, then `max_parallel`, then
  `min(max(4 × CPU cores, 10), 16)`. `max_parallel` now takes effect when the
  flag is omitted, and must be 1–100.
- **Exit codes and errors**: 0 ok, 1 runtime/network, 2 usage/config, 3 partial
  sync failure, 4 not found, 130 interrupted. Errors print a `hint:` line,
  `--verbose` adds context, and `--json` emits `{"error": {...}}` on stderr.
- **Line numbers**: `cat --line N` and `file:N` are 1-based, matching search
  and outline output. `--offset` still works with a warning until v0.6.0;
  replace `--offset N` with `--line N+1`.
- **`dox add`**: `--type` is inferred, `-r`/`-p`/`-u`/`-R` aliases exist, names
  must use letters, digits, `-` and `_`, and `--force` refuses sections it
  cannot rewrite safely.
- **Timeouts**: requests and sources time out after `timeout` (default `30s`;
  `dox sync --timeout`). Ctrl-C saves completed sources and exits 130.
- **Lock and manifests**: the lock moves to version 2 (adds `commit`; older
  locks load). Manifests regenerate automatically. The first sync after
  upgrading re-checks each GitHub source once without re-downloading
  unchanged files.
- **Homebrew and Docker**: the tap ships a cask (`brew uninstall --formula dox`
  first), and the image runs as UID 65532 (see [Docker](#docker)).

Notes for wrapper scripts and agent setups (such as agentx `mise` tasks):

- `dox add my-lib -r owner/repo -p docs` works without `--type`.
- `max_parallel` in `dox.toml` is honored, so remove per-call `--parallel`
  overrides that only worked around the old behavior.
- `GITHUB_TOKEN` is needed less often: a GitHub folder costs 2 API calls
  instead of one per file.
- Pass `--json` through and branch on exit codes (`2` means fix the config, `1`
  means retry later, `4` means a missing source or file).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
