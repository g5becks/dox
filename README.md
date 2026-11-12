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

```bash
dox init
```

2. Edit `dox.toml` and add sources.

3. Sync docs:

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

All commands accept `--config path/to/dox.toml` (`-c`) to override config discovery.

### sync

```bash
dox sync                    # Sync all sources
dox sync goreleaser hono    # Sync specific sources
dox sync --force            # Force re-download
dox sync --clean            # Clear owned output before syncing
dox sync --dry-run          # Preview without downloading
dox sync --parallel 5       # Override parallelism
dox sync --timeout 15s      # Override per-source timeout
```

### list

```bash
dox list                    # Show configured sources
dox list --verbose          # Show full details
dox list --json             # JSON output
dox list --files            # Show synced files
```

### add

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

```bash
dox clean                   # Remove all synced docs
dox clean goreleaser        # Remove specific source
```

### init

```bash
dox init                    # Create starter dox.toml
```

### collections

```bash
dox collections             # Table output
dox collections --json      # JSON output
dox collections --limit 5   # Limit results
```

### files

```bash
dox files goreleaser                       # Default table output
dox files goreleaser --json                # JSON output
dox files goreleaser --format csv          # CSV output
dox files goreleaser --fields path,type,size  # Custom fields
dox files goreleaser --limit 10            # Limit results
dox files goreleaser --all                 # Show all files (no limit)
dox files goreleaser --desc-length 100     # Shorter descriptions
```

Available fields: `path`, `type`, `lines`, `size`, `description`, `modified`

### cat

```bash
dox cat goreleaser docs/install.md                      # Show entire file
dox cat goreleaser docs/install.md --line 10            # Start at line 10
dox cat goreleaser docs/install.md --limit 20           # Show 20 lines
dox cat goreleaser docs/install.md:11 --limit 20       # Range
dox cat goreleaser docs/install.md --no-line-numbers    # No line numbers
dox cat goreleaser docs/install.md --json               # JSON output with metadata
```

Line locations in search results, outlines, `cat --line N`, and `file:N` use
1-based numbering. For example, `dox cat goreleaser docs/install.md:10 --limit 1`
reads the line numbered 10. JSON adds `line` for that starting line and retains
the legacy 0-based `offset` field. Use only one of `--line`, `file:N`, or
`--offset`. The old `--offset N` still works with a deprecation warning; migrate
to `--line N+1` before its removal in v0.6.0.

### outline

Show document structure — headings for markdown/MDX, exports for TypeScript:

```bash
dox outline goreleaser docs/install.md
dox outline goreleaser docs/install.md --json
```

Markdown/MDX outlines use source line positions, including frontmatter and MDX
imports, and recognize both ATX and setext headings. Re-run `dox sync` after
upgrading to regenerate manifests with corrected line numbers and metadata.
YAML/TOML metadata blocks decode up to 64 KiB; formatted heading/description
text has a 256 KiB per-file budget, after which normalized source text is kept.

### search

Search across documentation metadata or file contents.

**Metadata search** (default) — fuzzy matches across file paths, descriptions, headings, and exports:

```bash
dox search "installation"                    # Search all collections
dox search "hooks" --collection react        # Search specific collection
dox search "config" --limit 10               # Limit results
dox search "api" --json                      # JSON output
dox search "guide" --format csv              # CSV output
dox search "guide" --desc-length 80          # Truncate descriptions
```

**Content search** — literal or regex patterns within file contents:

```bash
dox search "useState" --content              # Literal (case-insensitive)
dox search "configuration" --content --collection goreleaser
dox search "func.*Logger" --content --regex  # Regex
dox search "import" --content --limit 20
```

Content search skips binary files and files over 50MB. Regex requires `--content`.

**Typical agent workflow:**

```bash
dox sync                                    # Ensure docs are current
dox collections --json                      # Discover available docs
dox search "authentication" --json          # Find relevant topics
dox files react --json --limit 100          # Browse collection files
dox cat react docs/hooks.md                 # Read specific content
```

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

## Config Reference

### Global

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `output` | string | `.dox` | Output directory root |
| `timeout` | string | `30s` | Per-source sync and HTTP request timeout (`time.ParseDuration`, > 0) |
| `github_token` | string | `$GITHUB_TOKEN` or `$GH_TOKEN` | GitHub API token for private repos and higher rate limits |
| `max_parallel` | int | `4 × CPU cores` (min 10, max 16) | Max concurrent source syncs |
| `max_per_host` | int | `16` | Max concurrent HTTP requests to one host across all sources (1–64) |
| `excludes` | []string | `[]` | Global exclude patterns applied to all git sources |

### Git Sources

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | Inferred from `repo` | `github` (other git hosts are planned for v0.5.0) |
| `repo` | Yes | — | Repository in `owner/repo` format |
| `host` | No | `github.com` | Git hosting domain; only `github.com` is supported in v0.4.0 |
| `path` | Yes | — | Path to directory or file in repo |
| `ref` | No | Default branch | Branch, tag, or commit SHA |
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

**Planned (v0.5.0):** GitLab, Codeberg/Gitea/Forgejo and self-hosted hosts
(including GitHub Enterprise). Until then, `type = "gitlab"`, `"codeberg"` or
`"git"`, and any `host` other than `github.com`, fail validation with
"not supported yet" (exit code 2) instead of silently querying github.com.

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
format = "table"              # Default format: table, json, csv
list_fields = ["path", "type", "lines", "size", "description"]
```

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
- GitHub token resolution: `github_token` in config → `GITHUB_TOKEN` env → `GH_TOKEN` env.
- Parallelism uses an explicit `--parallel` value first, then config `max_parallel`, then `min(max(4 × CPU cores, 10), 16)`. An omitted flag (or `--parallel 0`) uses the fallback; explicit positive flag/config values can exceed 16. Pass `--parallel 3` to keep the previous accidental limit.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
