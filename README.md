# dox

CLI tool that syncs, searches, and reads library docs locally.

## Install

### Homebrew (macOS/Linux)

```bash
brew install g5becks/tap/dox
```

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

### Go Install

```bash
go install github.com/g5becks/dox/cmd/dox@latest
```

### Build from Source

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

## Config

```toml
# dox.toml
output = ".dox"

# Default parallelism for syncing (default: 4x CPU cores, min 10)
# Override per-command with: dox sync --parallel N
max_parallel = 20

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
dox sync --clean            # Remove stale files after sync
dox sync --dry-run          # Preview without downloading
dox sync --parallel 5       # Override parallelism
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

# GitLab source (specify host)
dox add myproject --repo owner/repo --path docs --host gitlab.com

# Overwrite existing source
dox add goreleaser --repo goreleaser/goreleaser --path www/docs --force
```

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
dox cat goreleaser docs/install.md --offset 10          # Start at line 10
dox cat goreleaser docs/install.md --limit 20           # Show 20 lines
dox cat goreleaser docs/install.md --offset 10 --limit 20  # Range
dox cat goreleaser docs/install.md --no-line-numbers    # No line numbers
dox cat goreleaser docs/install.md --json               # JSON output with metadata
```

### outline

Show document structure — headings for markdown/MDX, exports for TypeScript:

```bash
dox outline goreleaser docs/install.md
dox outline goreleaser docs/install.md --json
```

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

## Config Reference

### Global

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `output` | string | `.dox` | Output directory root |
| `github_token` | string | `$GITHUB_TOKEN` or `$GH_TOKEN` | GitHub API token for private repos and higher rate limits |
| `max_parallel` | int | `4 × CPU cores` (min 10) | Max concurrent source syncs |
| `excludes` | []string | `[]` | Global exclude patterns applied to all git sources |

### Git Sources

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | Inferred from `repo` | `github`, `gitlab`, `codeberg`, or `git` |
| `repo` | Yes | — | Repository in `owner/repo` format |
| `host` | No | `github.com` | Git hosting domain |
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

# GitLab
[sources.docs]
repo = "owner/repo"
path = "docs"
host = "gitlab.com"

# Codeberg
[sources.docs]
repo = "owner/repo"
path = "docs"
host = "codeberg.org"

# Self-hosted (GitHub Enterprise, GitLab CE/EE, Gitea, etc.)
[sources.internal-docs]
repo = "company/documentation"
path = "guides"
host = "git.company.com"
```

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

- Global excludes apply to all git sources (GitHub, GitLab, Codeberg, self-hosted)
- Global excludes do **not** apply to URL sources
- Per-source `exclude` patterns add to (not replace) global excludes
- Duplicate patterns are automatically removed
- `dox init` generates a config with comprehensive defaults

## Notes

- Config discovery searches for `dox.toml` or `.dox.toml` from CWD up to filesystem root.
- Relative config paths resolve from the config file directory.
- GitHub token resolution: `github_token` in config → `GITHUB_TOKEN` env → `GH_TOKEN` env.
- Default parallelism is 4x CPU cores (min 10). Set `max_parallel` in config or use `--parallel` flag.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
