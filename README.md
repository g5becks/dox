# dox

`dox` is a Go CLI that syncs library and framework docs from GitHub repos or URLs into a local directory for indexing tools and AI assistants.

## Why

Keep documentation config in your repo, keep downloaded docs out of git, and reproduce docs locally on any machine with one command.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install g5becks/tap/dox
```

### Download Binary

Download pre-built binaries from the [releases page](https://github.com/g5becks/dox/releases).

**Linux/macOS:**
```bash
# Download and extract (adjust version and platform)
curl -L https://github.com/g5becks/dox/releases/latest/download/dox_Linux_x86_64.tar.gz | tar xz
sudo mv dox /usr/local/bin/
```

**Windows:**
Download the `.zip` from releases, extract, and add to PATH.

### Linux Packages

**Debian/Ubuntu:**
```bash
wget https://github.com/g5becks/dox/releases/latest/download/dox_0.1.0_linux_amd64.deb
sudo dpkg -i dox_0.1.0_linux_amd64.deb
```

**RedHat/Fedora/CentOS:**
```bash
wget https://github.com/g5becks/dox/releases/latest/download/dox_0.1.0_linux_amd64.rpm
sudo rpm -i dox_0.1.0_linux_amd64.rpm
```

**Alpine Linux:**
```bash
wget https://github.com/g5becks/dox/releases/latest/download/dox_0.1.0_linux_amd64.apk
sudo apk add --allow-untrusted dox_0.1.0_linux_amd64.apk
```

**Arch Linux:**
```bash
wget https://github.com/g5becks/dox/releases/latest/download/dox_0.1.0_linux_amd64.pkg.tar.zst
sudo pacman -U dox_0.1.0_linux_amd64.pkg.tar.zst
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

This generates `dox.toml` with sensible defaults, including global exclude patterns for common files (images, `node_modules`, build artifacts, etc.).

2. Edit `dox.toml` and add sources.

3. Sync docs:

```bash
dox sync
```

4. Ignore downloaded docs:

```gitignore
.dox/
```

## Example Config

```toml
# dox.toml
output = ".dox"

# Optional: Set default parallelism for syncing (default: 4x CPU cores, min 10)
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

# GitHub source - type inferred from 'repo'
[sources.goreleaser]
repo = "goreleaser/goreleaser"
path = "www/docs"
patterns = ["**/*.md"]  # Override default patterns
exclude = ["**/changelog.md"]  # Adds to global excludes

# URL source - type inferred from 'url'
[sources.hono]
url = "https://hono.dev/llms-full.txt"
```

## Commands

### Sync

```bash
dox sync
dox sync goreleaser hono
dox sync --force
dox sync --clean
dox sync --dry-run
dox sync --parallel 5
```

All commands accept `--config path/to/dox.toml` (`-c`) to override config discovery.

### List

```bash
dox list
dox list --verbose
dox list --json
dox list --files
```

### Add

```bash
# Add GitHub source (type inferred from --repo)
dox add goreleaser --repo goreleaser/goreleaser --path www/docs

# Add URL source (type inferred from --url)
dox add hono --url https://hono.dev/llms-full.txt

# Add GitLab source (specify host)
dox add myproject --repo owner/repo --path docs --host gitlab.com

# Overwrite existing source
dox add goreleaser --repo goreleaser/goreleaser --path www/docs --force
```

Use `--force` to overwrite an existing source with the same name.

### Clean

```bash
dox clean
dox clean goreleaser
```

### Init

```bash
dox init
```

## Global Excludes

Use the top-level `excludes` key to define patterns that apply to all git sources (GitHub, GitLab, Codeberg, etc.):

```toml
excludes = [
    "**/*.png",
    "**/*.jpg",
    "node_modules/**",
    ".vitepress/**",
]
```

**Key behaviors:**
- Global excludes apply to all git sources (GitHub, GitLab, Codeberg, self-hosted)
- Global excludes do NOT apply to URL sources
- Per-source `exclude` patterns add to (not replace) global excludes
- Duplicate patterns are automatically removed
- `dox init` generates a config with comprehensive defaults

**Example:**
```toml
excludes = ["**/*.png", "node_modules/**"]

[sources.docs]
repo = "owner/repo"
path = "docs"
exclude = ["**/*.jpg", "**/*.png"]  # Final excludes: *.png, *.jpg, node_modules/**
```

## Configuration Reference

### Minimal Configuration

`dox` infers source types automatically and applies sensible defaults to minimize config verbosity:

```toml
# Minimal GitHub source - type inferred from 'repo' presence
[sources.goreleaser]
repo = "goreleaser/goreleaser"
path = "www/docs"

# Minimal URL source - type inferred from 'url' presence
[sources.hono]
url = "https://hono.dev/llms-full.txt"
```

**What's inferred:**
- `type = "github"` when `repo` is present
- `type = "url"` when `url` is present
- `host = "github.com"` for git sources (GitHub default)
- `patterns = ["**/*.md", "**/*.mdx", "**/*.txt"]` for git sources
- `ref = <default branch>` for git sources (fetched from remote)
- `filename = <basename from URL>` for URL sources

### Git Hosting Sources

#### GitHub (default)

```toml
[sources.docs]
repo = "owner/repo"
path = "docs"
# type inferred as "github", host defaults to "github.com"
```

#### GitLab

```toml
[sources.docs]
repo = "owner/repo"
path = "docs"
host = "gitlab.com"  # Explicit host triggers gitlab type inference
```

Or explicitly set type:

```toml
[sources.docs]
type = "gitlab"
repo = "owner/repo"
path = "docs"
# host defaults to "github.com" but gitlab client handles gitlab.com URLs
```

#### Codeberg

```toml
[sources.docs]
repo = "owner/repo"
path = "docs"
host = "codeberg.org"
```

#### Self-Hosted Git (GitHub Enterprise, GitLab CE/EE, Gitea, etc.)

```toml
[sources.internal-docs]
repo = "company/documentation"
path = "guides"
host = "git.company.com"
```

#### Advanced Git Source Options

```toml
[sources.advanced]
type = "github"              # Optional: github|gitlab|codeberg|git (inferred if omitted)
repo = "owner/repo"          # Required: owner/repo format
host = "github.com"          # Optional: git host (default: github.com)
path = "docs/content"        # Required: path to directory or file in repo
ref = "v2.0.0"               # Optional: branch, tag, or commit SHA (default: repo default branch)
patterns = ["**/*.md"]       # Optional: glob patterns (default: ["**/*.md", "**/*.mdx", "**/*.txt"])
exclude = ["**/draft/**"]    # Optional: exclude patterns (merges with global excludes)
out = "custom-dir"           # Optional: output subdirectory (default: source name)
```

### URL Sources

For direct file downloads from any HTTP/HTTPS URL:

```toml
# Minimal
[sources.hono]
url = "https://hono.dev/llms-full.txt"

# With options
[sources.spec]
url = "https://example.com/api/spec.yaml"
filename = "openapi.yaml"   # Optional: custom filename (default: spec.yaml from URL)
out = "specs"               # Optional: output subdirectory (default: source name)
```

### Complete Field Reference

#### Global Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `output` | string | `.dox` | Output directory root (relative to config file or absolute) |
| `github_token` | string | `$GITHUB_TOKEN` or `$GH_TOKEN` | GitHub API token for private repos and higher rate limits |
| `max_parallel` | int | `4 × CPU cores` (min 10) | Max concurrent source syncs (I/O-bound operations) |
| `excludes` | []string | `[]` | Global exclude patterns applied to all git sources |

#### Source Fields (Git Hosting)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | Inferred | Source type: `github`, `gitlab`, `codeberg`, `git`, `url` |
| `repo` | Yes* | — | Repository in `owner/repo` format |
| `host` | No | `github.com` | Git hosting domain (e.g., `gitlab.com`, `git.company.com`) |
| `path` | Yes | — | Path to directory or file in repository |
| `ref` | No | Default branch | Branch, tag, or commit SHA to sync |
| `patterns` | No | `["**/*.md", "**/*.mdx", "**/*.txt"]` | Glob patterns for files to include |
| `exclude` | No | `[]` | Glob patterns to exclude (merged with global `excludes`) |
| `out` | No | Source name | Custom output subdirectory name |

*Required for git sources. Must have either `repo` OR `url`, not both.

#### Source Fields (URL)

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | `url` (inferred) | Automatically set to `url` when `url` field is present |
| `url` | Yes* | — | Direct HTTP/HTTPS URL to a file |
| `filename` | No | Basename from URL | Custom filename for downloaded file |
| `out` | No | Source name | Custom output subdirectory name |

*Required for URL sources. Must have either `repo` OR `url`, not both.

### Configuration Comparison: Before vs. After

**Before** (verbose, redundant):
```toml
[sources.goreleaser]
type = "github"              # Redundant - obvious from 'repo'
repo = "goreleaser/goreleaser"
ref = "main"                 # Redundant - same as default branch
path = "www/docs"
patterns = ["**/*.md", "**/*.mdx", "**/*.txt"]  # Redundant - same as default

[sources.hono]
type = "url"                 # Redundant - obvious from 'url'
url = "https://hono.dev/llms-full.txt"
filename = "llms-full.txt"   # Redundant - same as URL basename
```

**After** (slim, clear):
```toml
[sources.goreleaser]
repo = "goreleaser/goreleaser"
path = "www/docs"

[sources.hono]
url = "https://hono.dev/llms-full.txt"
```

Both configs produce identical behavior. The slimmer version relies on type inference and sensible defaults.

## Output Layout

Default output root is `.dox/`:

```text
.dox/
  .dox.lock
  goreleaser/
  hono/
```

Each source writes into its own directory (source name by default, or `out` override).

## Integrations

`dox` downloads docs to a local directory that can be indexed by various tools:

### AI Code Search & RAG

- **[ck-search](https://github.com/BeaconBay/ck)** — Semantic code search powered by embeddings. Index your `.dox/` directory for context-aware documentation search.
  ```bash
  dox sync
  ck index .dox/
  ck search "how to configure goreleaser"
  ```

- **[kit](https://github.com/cased/kit)** — AI-powered development assistant. Point kit at your synced docs for enhanced context.
  ```bash
  dox sync
  kit configure --docs-path .dox/
  ```

- **[aichat RAG](https://github.com/sigoden/aichat/wiki/RAG-Guide)** — Retrieval-Augmented Generation for LLMs. Build a RAG system from your documentation.
  ```bash
  dox sync
  aichat --rag .dox/
  ```

### Documentation Indexers

- **[Codana](https://codana.dev)** — Documentation search and indexing:
  ```bash
  dox sync
  codana documents add-collection goreleaser .dox/goreleaser/
  codana documents add-collection hono .dox/hono/
  codana documents index
  ```

- **[Meilisearch](https://www.meilisearch.com/)** — Fast, typo-tolerant search engine:
  ```bash
  dox sync
  # Index .dox/ directory with Meilisearch
  ```

- **[Typesense](https://typesense.org/)** — Open-source search engine:
  ```bash
  dox sync
  # Index .dox/ with Typesense for fast doc search
  ```

### IDE & Editor Extensions

Configure your editor to use `.dox/` as a documentation source:
- VSCode: Point Copilot or Codeium context to `.dox/`
- Cursor: Add `.dox/` to workspace context
- Vim/Neovim: Use with coc.nvim or native LSP doc providers

### Custom Scripts

Since `dox` outputs plain files, you can build custom tooling:
```bash
# Full-text search
rg "pattern" .dox/

# Convert markdown to HTML
find .dox/ -name "*.md" -exec pandoc -o {}.html {} \;

# Generate embeddings
python embed_docs.py .dox/
```

## Notes

- Config discovery searches for `dox.toml` or `.dox.toml` from CWD up to filesystem root.
- Relative config paths resolve from the config file directory.
- GitHub token resolution order:
  1. `github_token` in config
  2. `GITHUB_TOKEN`
  3. `GH_TOKEN`
- **Parallelism**: Default is 4x CPU cores (minimum 10) since syncing is I/O-bound. Set `max_parallel` in config or use `--parallel` flag to override.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT
