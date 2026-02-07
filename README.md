# dox

`dox` is a Go CLI that syncs library and framework docs from GitHub repos or URLs into a local directory for indexing tools like Codana.

## Why

Keep documentation config in your repo, keep downloaded docs out of git, and reproduce docs locally on any machine with one command.

## Installation

### Go install

```bash
go install github.com/g5becks/dox/cmd/dox@latest
```

### Build from source

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

## Codana Workflow

```bash
dox sync
codana documents add-collection goreleaser .dox/goreleaser/
codana documents add-collection hono .dox/hono/
codana documents index
```

Codana: https://codana.dev

## Notes

- Config discovery searches for `dox.toml` or `.dox.toml` from CWD up to filesystem root.
- Relative config paths resolve from the config file directory.
- GitHub token resolution order:
  1. `github_token` in config
  2. `GITHUB_TOKEN`
  3. `GH_TOKEN`
