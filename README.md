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

### Package Managers

**Debian/Ubuntu:**
```bash
# Download .deb from releases page
wget https://github.com/g5becks/dox/releases/latest/download/dox_amd64.deb
sudo dpkg -i dox_amd64.deb
```

**RedHat/Fedora/CentOS:**
```bash
# Download .rpm from releases page
wget https://github.com/g5becks/dox/releases/latest/download/dox_amd64.rpm
sudo rpm -i dox_amd64.rpm
```

**Arch Linux (AUR):**
```bash
yay -S dox-bin
# or
paru -S dox-bin
```

**Chocolatey (Windows):**
```powershell
choco install dox
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

[sources.goreleaser]
type = "github"
repo = "goreleaser/goreleaser"
path = "www/docs"
patterns = ["**/*.md"]

[sources.hono]
type = "url"
url = "https://hono.dev/llms-full.txt"
filename = "hono.txt"
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
dox add goreleaser --type github --repo goreleaser/goreleaser --path www/docs
dox add hono --type url --url https://hono.dev/llms-full.txt --filename hono.txt
dox add goreleaser --type github --repo goreleaser/goreleaser --path www/docs --force
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

## Source Types

### `github`

Required:
- `repo`
- `path`

Optional:
- `ref`
- `patterns`
- `exclude`
- `out`

### `url`

Required:
- `url`

Optional:
- `filename`
- `out`

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
