# dox - Implementation Plan

## 1. Overview

**dox** is a CLI tool that downloads library/framework documentation from configured sources to a local directory. Its primary purpose is to feed documentation into indexing tools like [Codana](https://codana.dev) so AI agents can search them as a local RAG.

**Core principle**: Config file in your repo, docs gitignored. On a new machine, run `dox sync` and all your library docs appear locally, ready to be indexed.

### What dox is NOT

- Not a search engine (Codana does that)
- Not an llms.txt generator (pivot away from the original llxt concept)
- Not a documentation viewer

---

## 2. Config Format

Config file: `dox.toml` or `.dox.toml` (searched from CWD up the directory tree).

```toml
# Global output directory (default: ".dox", relative to config file location)
# output = ".dox"

# Optional GitHub token for higher rate limits (60/hr unauth vs 5000/hr auth)
# Can also be set via GITHUB_TOKEN or GH_TOKEN env vars
# github_token = "ghp_..."

# --- GitHub directory source ---
[sources.goreleaser]
type = "github"
repo = "goreleaser/goreleaser"       # owner/repo
path = "www/docs"                     # directory within repo
ref = "main"                          # branch, tag, or commit SHA (default: repo default branch)
patterns = ["**/*.md"]                # glob patterns (default: ["**/*.md", "**/*.mdx", "**/*.txt"])
exclude = ["**/changelog.md", "**/CONTRIBUTING.md"]  # glob patterns to exclude (default: [])
# out = "custom/goreleaser"           # override output subdirectory (default: source key name)

# --- GitHub single file source ---
[sources.tanstack-db]
type = "github"
repo = "TanStack/db"
path = "docs/overview.md"            # when path has a doc extension, treated as single file
ref = "main"

# --- URL source (single file download) ---
[sources.hono]
type = "url"
url = "https://hono.dev/llms-full.txt"
filename = "hono.txt"                 # output filename (default: derived from URL basename)

# --- URL source (llms.txt) ---
[sources.effect]
type = "url"
url = "https://effect.website/llms-full.txt"
filename = "effect.txt"

# --- Future: GitLab source ---
# [sources.my-gitlab-lib]
# type = "gitlab"
# host = "gitlab.com"                  # or self-hosted URL
# repo = "org/project"
# path = "docs"

# --- Future: Gitea/Codeberg source ---
# [sources.my-codeberg-lib]
# type = "gitea"
# host = "codeberg.org"
# repo = "org/project"
# path = "docs"
```

### Config Resolution

1. Check CWD for `dox.toml`, then `.dox.toml`
2. Walk up parent directories repeating step 1
3. Stop at filesystem root
4. All relative paths in config (like `output`, `out`) resolve relative to the config file's directory, not CWD

### Output Directory Resolution

For each source, the output directory is determined by:

1. If the source has `out = "custom/path"` → use that (relative to global output dir)
2. Otherwise → use the source's key name (e.g., `[sources.goreleaser]` → `goreleaser/`)

The global `output` defaults to `.dox` if not specified.

**Example**: with default `output = ".dox"` and `[sources.goreleaser]`:
→ files land in `.dox/goreleaser/`

**Example**: with `output = ".dox"` and `[sources.goreleaser]` + `out = "go/goreleaser"`:
→ files land in `.dox/go/goreleaser/`

### Source Types

| Type     | Description                          | Required Fields        | Optional Fields                        |
|----------|--------------------------------------|------------------------|----------------------------------------|
| `github` | Download from a GitHub repo          | `repo`, `path`         | `ref`, `patterns`, `exclude`, `out`    |
| `url`    | Download a single file from any URL  | `url`                  | `filename`, `out`                      |

**Planned (v2+):**

| Type     | Description                          | Required Fields              | Optional Fields                        |
|----------|--------------------------------------|------------------------------|----------------------------------------|
| `gitlab` | Download from a GitLab repo          | `repo`, `path`, `host`       | `ref`, `patterns`, `exclude`, `out`    |
| `gitea`  | Download from Gitea/Codeberg/Forgejo | `repo`, `path`, `host`       | `ref`, `patterns`, `exclude`, `out`    |

All git forge types share the same conceptual model: tree listing, pattern matching, file downloading. The `host` field distinguishes instances (GitHub doesn't need it since `github.com` is the implicit default, though `host` could be added later for GitHub Enterprise).

### Pattern Matching (git forge sources only)

- Uses [doublestar](https://github.com/bmatcuk/doublestar) glob syntax
- Applied against file paths relative to the configured `path`
- Default patterns: `["**/*.md", "**/*.mdx", "**/*.txt"]`
- Default excludes: `[]`
- Patterns are matched against the file tree returned by the forge's API (no local filesystem involved)

### Single-File vs Directory Detection (GitHub sources)

When `path` ends with a known documentation extension (`.md`, `.mdx`, `.txt`, `.rst`), it is treated as a **single file**. Otherwise, it is treated as a **directory**.

- **Directory path**: `path = "www/docs"` → Trees API, pattern matching, multiple files
- **Single file path**: `path = "docs/overview.md"` → direct raw download, one file

If this heuristic fails for an edge case (e.g., a directory literally named `weird.md/`), the user can add a trailing slash to force directory mode: `path = "weird.md/"`.

### GitHub Token Resolution Order

1. `github_token` field in config file
2. `GITHUB_TOKEN` environment variable
3. `GH_TOKEN` environment variable
4. Unauthenticated (60 requests/hour)

---

## 3. Freshness Checking & Lock File

To avoid unnecessary downloads, dox maintains a lock file at `{output}/.dox.lock` (JSON format). This file is **gitignored** alongside the output directory.

### Lock File Format

```json
{
  "version": 1,
  "sources": {
    "goreleaser": {
      "type": "github",
      "tree_sha": "abc123def456789",
      "ref_resolved": "main",
      "synced_at": "2024-01-15T10:30:00Z",
      "files": {
        "getting-started.md": "blobsha1",
        "customization/build.md": "blobsha2",
        "customization/release.md": "blobsha3"
      }
    },
    "hono": {
      "type": "url",
      "etag": "\"W/abc123\"",
      "last_modified": "Tue, 15 Jan 2024 10:30:00 GMT",
      "synced_at": "2024-01-15T10:30:00Z"
    }
  }
}
```

### Freshness Strategy by Source Type

**GitHub (and future git forge) sources:**

The GitHub Trees API returns a SHA for the tree and individual blob SHAs for every file. This makes smart diffing cheap — the tree fetch is already required to enumerate files, so freshness checking adds **zero extra API calls**.

1. Fetch tree from GitHub API → get new tree SHA
2. Compare against lock file's `tree_sha`
3. **If same** → skip entirely, print `goreleaser: up to date`
4. **If different** (or no lock entry):
   a. Diff old `files` map against new tree entries (by blob SHA)
   b. Download only files where blob SHA changed or file is new
   c. Delete local files that no longer exist in remote tree
   d. Update lock file with new tree SHA and file map
5. **If `--force` flag** → ignore lock file, re-download everything

This approach is efficient even for large doc trees. A repo with 200 doc files where only 3 changed results in 3 downloads instead of 200.

**URL sources:**

HTTP conditional requests avoid re-downloading unchanged files.

1. If lock file has `etag` or `last_modified` for this source:
   - Send GET with `If-None-Match: <etag>` and/or `If-Modified-Since: <last_modified>`
   - **If 304 Not Modified** → skip, print `hono: up to date`
   - **If 200** → download, update lock file
2. If no lock entry → download, store etag/last_modified from response headers
3. If server doesn't send etag or last-modified headers → always re-download (no way to check freshness)

**Summary:**

| Source Type | Freshness Signal      | Extra API Cost | Granularity    |
|-------------|-----------------------|----------------|----------------|
| GitHub      | Tree SHA + blob SHAs  | None (free)    | Per-file diff  |
| URL         | ETag / Last-Modified  | None (free)    | Whole file     |
| GitLab (v2) | Tree SHA + blob SHAs  | None (free)    | Per-file diff  |
| Gitea (v2)  | Tree SHA + blob SHAs  | None (free)    | Per-file diff  |

---

## 4. CLI Commands

Built with [urfave/cli v3](https://cli.urfave.org/).

### `dox sync`

Download/update all configured sources, skipping sources that haven't changed.

```
dox sync                      # sync all sources (with freshness checking)
dox sync goreleaser hono      # sync specific sources only
dox sync --force              # ignore lock file, re-download everything
dox sync --force goreleaser   # force re-sync just goreleaser
dox sync --clean              # remove output dir entirely before syncing
dox sync --dry-run            # show what would be downloaded without downloading
```

**Flags:**
- `--config, -c <path>` — explicit config file path (skips directory search)
- `--force, -f` — ignore lock file, re-download everything
- `--clean` — remove the entire output directory before syncing
- `--dry-run` — show what would happen without making changes
- `--parallel, -p <n>` — max concurrent source downloads (default: 3)

### `dox list`

List configured sources and their sync status.

```
dox list                      # default table output
dox list --json               # JSON output (for scripting / AI agents)
dox list --verbose            # include patterns, ref, excludes
dox list --files              # include per-source file counts
```

**Default output** (go-pretty table):
```
╭────────────────┬────────┬──────────────────────────────────┬─────────────────────╮
│ SOURCE         │ TYPE   │ LOCATION                         │ STATUS              │
├────────────────┼────────┼──────────────────────────────────┼─────────────────────┤
│ goreleaser     │ github │ goreleaser/goreleaser/www/docs    │ synced (42 files)   │
│ hono           │ url    │ https://hono.dev/llms-full.txt   │ synced              │
│ tanstack-db    │ github │ TanStack/db/docs                 │ not synced          │
╰────────────────┴────────┴──────────────────────────────────┴─────────────────────╯
```

**`--verbose` output** adds columns: `REF`, `PATTERNS`, `OUTPUT DIR`

**`--json` output:**
```json
[
  {
    "name": "goreleaser",
    "type": "github",
    "repo": "goreleaser/goreleaser",
    "path": "www/docs",
    "ref": "main",
    "output_dir": ".dox/goreleaser",
    "status": "synced",
    "file_count": 42,
    "synced_at": "2024-01-15T10:30:00Z"
  }
]
```

**Flags:**
- `--json` — output as JSON array
- `--verbose, -v` — show all source fields
- `--files` — scan output directories and include file counts (default: only show counts when lock data exists)
- `--config, -c <path>` — explicit config file path

### `dox add`

Add a new source to the config file. Designed for both human use and AI agent use.

```
# Add a GitHub directory source
dox add goreleaser --type github --repo goreleaser/goreleaser --path www/docs

# Add a GitHub source with options
dox add effect --type github --repo Effect-TS/effect --path docs --ref main --patterns "**/*.mdx"

# Add a URL source
dox add hono --type url --url https://hono.dev/llms-full.txt --filename hono.txt

# Add with custom output directory
dox add my-lib --type github --repo org/repo --path docs --out custom/my-lib

# Add with exclusions
dox add big-lib --type github --repo org/big --path docs --exclude "**/changelog.md" --exclude "**/CONTRIBUTING.md"
```

**Positional arg:** `<name>` — the source key name (becomes `[sources.<name>]`)

**Flags:**
- `--type, -t <type>` — source type: `github`, `url` (required)
- `--repo <owner/repo>` — repository (github)
- `--path <path>` — path within repo (github)
- `--ref <ref>` — branch/tag/SHA (github)
- `--patterns <glob>` — include patterns, repeatable (github)
- `--exclude <glob>` — exclude patterns, repeatable (github)
- `--url <url>` — download URL (url type)
- `--filename <name>` — output filename (url type)
- `--out <path>` — custom output subdirectory
- `--config, -c <path>` — explicit config file path

**Behavior:**
- Validates required fields for the given type before writing
- Errors if source name already exists (use `--force` to overwrite)
- **Append strategy**: Reads the existing config file as raw text, builds the new `[sources.<name>]` TOML section as a string (manually formatted, not via full TOML marshal/unmarshal), and appends it to the end of the file. This preserves all existing comments and formatting perfectly since we never parse and rewrite the existing content
- If `--force` is used and the source already exists, the existing section must be located and replaced. For v1, this can use a simple text-based approach: find `[sources.<name>]` and replace everything up to the next `[sources.` or EOF. If this proves fragile, fall back to requiring manual edit for overwrites

### `dox clean`

Remove downloaded docs.

```
dox clean                     # remove entire output dir (including lock file)
dox clean goreleaser          # remove only goreleaser's subdirectory + its lock entry
dox clean goreleaser hono     # remove multiple sources
```

### `dox init`

Create a starter `dox.toml` in the current directory.

```
dox init                      # creates dox.toml with commented-out examples
```

Errors if `dox.toml` or `.dox.toml` already exists in CWD.

### Global Flags

- `--version` — print version
- `--help, -h` — print help

---

## 5. Architecture

### Package Structure

```
cmd/
  dox/
    main.go                   # Entry point, version vars, CLI setup

internal/
  config/
    config.go                 # Config loading with koanf, directory walking, validation
    types.go                  # Config struct definitions + defaults

  source/
    source.go                 # Source interface + factory function
    github.go                 # GitHub source (Trees API + raw downloads)
    url.go                    # URL source (simple GET with conditional requests)

  lockfile/
    lockfile.go               # Lock file read/write + per-source entry types

  sync/
    sync.go                   # Orchestrates downloading across sources with worker pool

  ui/
    progress.go               # go-pretty progress bar rendering
    table.go                  # go-pretty table output for list command
```

### Dependency Flow

```
cmd/dox/main.go
  ├─► internal/config         (koanf + toml parser)
  ├─► internal/lockfile       (JSON read/write)
  ├─► internal/sync           (orchestration)
  │     ├─► internal/source   (resty HTTP client)
  │     ├─► internal/lockfile
  │     └─► internal/ui       (go-pretty progress)
  └─► internal/ui             (go-pretty tables for list)
```

### Extensibility for Future Forge Types

The source interface is designed so that adding GitLab/Gitea/Codeberg support later means:

1. Add a new file (e.g., `internal/source/gitlab.go`)
2. Implement the `Source` interface
3. Register the type in the factory function
4. Add config validation for the new type

All git forge sources share the same conceptual model (tree listing → pattern matching → file downloads → blob SHAs for freshness), so common logic can be extracted into a shared helper if/when a second forge is added. For v1, keep it simple — no premature abstraction.

---

## 6. Implementation Details

### 6.1 `cmd/dox/main.go`

```go
// Build-time variables (set via ldflags)
var (
    version   = "dev"
    commit    = "unknown"
    buildTime = "unknown"
)
```

- Create root `cli.Command` with `sync`, `list`, `clean`, `init`, `add` subcommands
- Pass `context.Context` through for cancellation support (Ctrl+C graceful shutdown)
- Call `cmd.Run(context.Background(), os.Args)`

### 6.2 `internal/config`

**Config structs (`types.go`):**

```go
type Config struct {
    Output      string            `koanf:"output"`
    GitHubToken string            `koanf:"github_token"`
    Sources     map[string]Source `koanf:"sources"`

    // Set after loading — not from config file
    ConfigDir string `koanf:"-"` // directory containing the config file
}

type Source struct {
    Type     string   `koanf:"type"`     // "github", "url" (future: "gitlab", "gitea")
    Repo     string   `koanf:"repo"`     // owner/repo (git forge types)
    Path     string   `koanf:"path"`     // path within repo (git forge types)
    Ref      string   `koanf:"ref"`      // branch/tag/sha (git forge types)
    Host     string   `koanf:"host"`     // forge host (future: gitlab, gitea)
    Patterns []string `koanf:"patterns"` // include globs (git forge types)
    Exclude  []string `koanf:"exclude"`  // exclude globs (git forge types)
    URL      string   `koanf:"url"`      // download URL (url type)
    Filename string   `koanf:"filename"` // output filename (url type)
    Out      string   `koanf:"out"`      // custom output subdirectory override
}
```

**Defaults:**

```go
const (
    DefaultOutput = ".dox"
)

var DefaultPatterns = []string{"**/*.md", "**/*.mdx", "**/*.txt"}
```

**Config loading flow (`config.go`):**

1. If `--config` flag provided, load that file directly
2. Otherwise, walk up from CWD looking for `dox.toml` or `.dox.toml`
3. Load file via koanf with `file.Provider()` + `toml.Parser()`
4. Unmarshal into `Config` struct
5. Set `ConfigDir` to the directory containing the found config file
6. Apply defaults:
   - `output` → `.dox` if empty
   - `patterns` → `DefaultPatterns` if empty for each git forge source
7. Validate:
   - Each source has a valid `type`
   - GitHub sources have `repo` and `path`
   - URL sources have `url`
   - `repo` matches `owner/name` format
8. Resolve `output` path relative to `ConfigDir`

**`OutputDir` helper** (resolves per-source output path):

```go
func (c *Config) OutputDir(sourceName string, source Source) string {
    base := filepath.Join(c.ConfigDir, c.Output)
    if source.Out != "" {
        return filepath.Join(base, source.Out)
    }
    return filepath.Join(base, sourceName)
}
```

**Directory walk implementation:**

```go
func FindConfigFile() (string, error) {
    dir, err := os.Getwd()
    if err != nil {
        return "", oops.Wrapf(err, "getting working directory")
    }

    names := []string{"dox.toml", ".dox.toml"}

    for {
        for _, name := range names {
            p := filepath.Join(dir, name)
            if _, err := os.Stat(p); err == nil {
                return p, nil
            }
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            return "", oops.
                Code("CONFIG_NOT_FOUND").
                Hint("Run 'dox init' to create a config file").
                Errorf("no dox.toml or .dox.toml found in any parent directory")
        }
        dir = parent
    }
}
```

### 6.3 `internal/lockfile`

**Types:**

```go
type LockFile struct {
    Version int                    `json:"version"`
    Sources map[string]*LockEntry `json:"sources"`
}

type LockEntry struct {
    Type        string            `json:"type"`
    TreeSHA     string            `json:"tree_sha,omitempty"`      // git forge sources
    RefResolved string            `json:"ref_resolved,omitempty"`  // git forge sources
    ETag        string            `json:"etag,omitempty"`          // url sources
    LastMod     string            `json:"last_modified,omitempty"` // url sources
    SyncedAt    time.Time         `json:"synced_at"`
    Files       map[string]string `json:"files,omitempty"`         // relative path → blob SHA
}
```

**Operations:**

- `Load(outputDir string) (*LockFile, error)` — read `.dox.lock` from output dir, return empty if not exists
- `Save(outputDir string) error` — write `.dox.lock` atomically (write temp, rename)
- `GetEntry(name string) *LockEntry` — get lock entry for a source (nil if absent)
- `SetEntry(name string, entry *LockEntry)` — update/create lock entry
- `RemoveEntry(name string)` — remove a source's lock entry

### 6.4 `internal/source`

**Source interface (`source.go`):**

```go
// SyncResult reports what happened during a sync.
type SyncResult struct {
    Downloaded int       // files downloaded
    Deleted    int       // files removed (no longer in remote)
    Skipped    bool      // true if source was up-to-date
    LockEntry  *lockfile.LockEntry // updated lock state to persist
}

// SyncOptions controls the behavior of a source sync.
type SyncOptions struct {
    Force  bool // skip freshness checks, re-download everything
    DryRun bool // compute diff but don't download/delete
}

// Source defines a documentation source that can be synced.
type Source interface {
    // Sync downloads/updates files for this source into destDir.
    // prevLock is the previous lock entry (nil on first sync).
    Sync(ctx context.Context, destDir string, prevLock *lockfile.LockEntry, opts SyncOptions, tracker *progress.Tracker) (*SyncResult, error)
}

// New creates a Source from config. Returns an error for unknown types.
func New(name string, cfg config.Source, token string) (Source, error) {
    switch cfg.Type {
    case "github":
        return NewGitHub(name, cfg, token)
    case "url":
        return NewURL(name, cfg)
    default:
        return nil, oops.
            Code("UNKNOWN_SOURCE_TYPE").
            With("type", cfg.Type).
            Hint("Supported types: github, url").
            Errorf("unknown source type %q for source %q", cfg.Type, name)
    }
}
```

**GitHub implementation (`github.go`):**

The GitHub source uses two API endpoints via resty:

1. **Trees API** — list all files in the repo:
   ```
   GET https://api.github.com/repos/{owner}/{repo}/git/trees/{ref}?recursive=1
   ```
   - Response includes `tree[]` with `path`, `type` ("blob" or "tree"), `sha`, `size`
   - Filter entries: `type == "blob"`, path starts with configured `path`, matches patterns, not excluded

2. **Raw content download** — fetch each matching file:
   ```
   GET https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}
   ```
   - Save response body to local file using **atomic writes** (write to `{dest}.tmp`, then `os.Rename` to final path). This prevents partial files on crash or cancellation
   - Maintain directory structure under `{destDir}/` (create parent dirs with `os.MkdirAll`)

**Trees API truncation:** If the tree has >100,000 entries, GitHub returns `truncated: true`. This is extremely unlikely for documentation directories, but if encountered, dox should log a warning and fall back to the Contents API (recursive directory listing) for that source.

**Single file mode:** When path has a doc extension, skip the Trees API entirely. Download the single file directly via raw URL. Store its blob SHA from a Contents API call (or just re-download each time — single files are cheap).

**Ref resolution:**
- If `ref` is not specified, call `GET /repos/{owner}/{repo}` to get `default_branch`
- Cache the resolved ref for the duration of the sync

**Sync logic (directory mode):**

```
fetchTree(ref) → newTree
if prevLock != nil && prevLock.TreeSHA == newTree.SHA:
    return Skipped

buildFileMap(newTree, path, patterns, exclude) → newFiles  // map[relativePath]blobSHA
oldFiles = prevLock.Files (or empty map)

toDownload = files in newFiles where SHA differs from oldFiles or not in oldFiles
toDelete   = files in oldFiles not in newFiles

download(toDownload) with progress tracking
delete(toDelete) from disk + clean up empty dirs

return SyncResult{
    Downloaded: len(toDownload),
    Deleted:    len(toDelete),
    LockEntry: { TreeSHA: newTree.SHA, Files: newFiles, ... }
}
```

**Pattern matching:**
- Use `github.com/bmatcuk/doublestar/v4` for glob matching
- Match against the file path relative to the configured `path` prefix
- Apply include patterns first (must match at least one), then exclude patterns (must not match any)

**Resty client setup:**

```go
// newGitHubClient creates a resty client for the GitHub API.
// The caller is responsible for calling client.Close() when done.
func newGitHubClient(token string) *resty.Client {
    client := resty.New()

    client.SetBaseURL("https://api.github.com")
    client.SetHeader("Accept", "application/vnd.github.v3+json")
    client.SetHeader("User-Agent", "dox/"+version)
    client.SetRetryCount(3)
    client.SetRetryWaitTime(1 * time.Second)
    client.SetRetryMaxWaitTime(5 * time.Second)

    if token != "" {
        client.SetAuthToken(token)
    }

    return client
}
```

**Important:** The caller must `defer client.Close()` — not the constructor. The resty client is created once per GitHub source and closed after the sync completes.

**Rate limit handling:**
- Check `X-RateLimit-Remaining` header on responses
- If approaching zero, log a warning suggesting token configuration
- Resty's built-in retry handles 429 responses automatically

**URL implementation (`url.go`):**

```
if prevLock != nil && (prevLock.ETag != "" || prevLock.LastMod != ""):
    send conditional GET (If-None-Match / If-Modified-Since)
    if 304: return Skipped

download file to {destDir}/{filename}
extract ETag and Last-Modified from response headers
return SyncResult with updated lock entry
```

Filename resolution: `source.Filename` if set, otherwise `path.Base(url)`.

### 6.5 `internal/sync`

**Sync orchestrator (`sync.go`):**

```go
func Run(ctx context.Context, cfg *config.Config, opts SyncOptions) error

type SyncOptions struct {
    SourceNames []string // empty = all sources
    Force       bool
    DryRun      bool
    MaxParallel int      // default: 3
    Clean       bool
}
```

1. If `Clean`, remove the entire output directory
2. Load lock file from output directory
3. Resolve which sources to sync (all if no names given, or filter by names)
4. Create go-pretty progress writer, start rendering
5. Launch worker pool using `errgroup.Group` with `SetLimit(MaxParallel)`
6. For each source:
   a. Create a `progress.Tracker` and append to progress writer
   b. Dispatch to errgroup: create Source, call `Sync()`, collect result
7. Wait for all workers
8. Stop progress writer
9. Save updated lock file
10. Print summary table: sources synced, files downloaded, files deleted, sources skipped, errors

**Dry run:** The `dryRun` flag is passed through to `Source.Sync()`. In dry-run mode, each source computes the diff (fetches tree, compares with lock) but does not download or delete files. It returns a `SyncResult` with the counts of what *would* change. The orchestrator prints these results as a summary table.

**Concurrency and lock file safety:** Workers run in parallel but do NOT write to the lock file directly. Each worker returns a `SyncResult` containing an updated `*LockEntry`. The orchestrator collects all results after the errgroup completes, then writes the lock file once in a single-threaded pass (step 9). No mutex needed on the lock file itself.

**Signal handling (graceful shutdown):**

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
cmd.Run(ctx, os.Args)
```

When Ctrl+C is pressed, the context is cancelled. This propagates through:
- `errgroup` stops dispatching new workers
- In-flight resty requests are cancelled via their `SetContext(ctx)`
- Partial file downloads are cleaned up (see atomic writes below)
- The lock file is saved with whatever results completed successfully before cancellation

### 6.6 `internal/ui`

**Progress (`progress.go`):**

Using go-pretty's progress writer to show per-source download progress:

```go
func NewProgressWriter() progress.Writer {
    pw := progress.NewWriter()
    pw.SetAutoStop(true)
    pw.SetTrackerLength(30)
    pw.SetStyle(progress.StyleBlocks)
    pw.Style().Visibility.ETA = true
    pw.Style().Visibility.Speed = true
    pw.Style().Visibility.Value = true
    return pw
}
```

Each source gets its own tracker:
- GitHub directory sources: total = number of files to download (set after tree fetch + diff)
- GitHub single file sources: indeterminate until downloaded
- URL sources: total = Content-Length from response headers (or indeterminate)
- Sources that are up-to-date: tracker immediately marked done with "up to date" message

**Table (`table.go`):**

Configurable table rendering for `dox list`:

```go
type ListOptions struct {
    JSON    bool
    Verbose bool
    Files   bool
}

func RenderSourceList(sources []SourceStatus, opts ListOptions)
```

- Default: rounded table style with Source, Type, Location, Status columns
- `--verbose`: adds Ref, Patterns, Output Dir columns
- `--json`: marshals to JSON array, prints to stdout
- `--files`: scans output directories for actual file counts

---

## 7. Error Handling

Use [oops](https://github.com/samber/oops) throughout for structured errors with context.

**Error codes:**

| Code                  | When                                                    |
|-----------------------|---------------------------------------------------------|
| `CONFIG_NOT_FOUND`    | No config file found in any parent directory            |
| `CONFIG_INVALID`      | Config has invalid syntax or missing/invalid fields     |
| `CONFIG_WRITE_ERROR`  | Failed to write config file (add/init commands)         |
| `SOURCE_NOT_FOUND`    | Named source doesn't exist in config                   |
| `SOURCE_EXISTS`       | Source name already exists (dox add without --force)    |
| `UNKNOWN_SOURCE_TYPE` | Config has unrecognized source type                     |
| `GITHUB_API_ERROR`    | GitHub API returned a non-success response              |
| `GITHUB_RATE_LIMIT`   | Hit GitHub rate limit (X-RateLimit-Remaining: 0)        |
| `DOWNLOAD_FAILED`     | Failed to download a file                               |
| `WRITE_FAILED`        | Failed to write file to disk                            |
| `LOCK_ERROR`          | Failed to read/write lock file                          |

**Pattern:**

```go
return oops.
    Code("GITHUB_API_ERROR").
    With("repo", source.Repo).
    With("status", resp.StatusCode()).
    Hint("Check that the repository exists and is public, or configure a github_token").
    Wrapf(err, "fetching tree for %s", source.Repo)
```

**Non-fatal error collection:** When syncing multiple sources, one source failing should not abort the others. The orchestrator collects per-source errors and reports them all in the summary.

**Exit codes:**

| Code | Meaning                                    |
|------|--------------------------------------------|
| 0    | Success (all sources synced or up-to-date) |
| 1    | Partial failure (some sources failed)      |
| 2    | Fatal error (config not found, invalid config, etc.) |

---

## 8. On-Disk Layout

After `dox sync`, the output directory looks like:

```
.dox/                           # default output directory
  .dox.lock                     # lock file (freshness metadata)
  goreleaser/                   # source key name = subdirectory
    getting-started.md
    customization/
      build.md
      release.md
  hono/                         # source key name = subdirectory
    hono.txt                    # single file from URL source
  tanstack-db/
    overview.md                 # single file from GitHub
```

- Each source gets its own subdirectory (named by source key or `out` override)
- GitHub directory sources preserve the relative directory structure (relative to the configured `path`)
- URL sources place the file directly in the source subdirectory
- The lock file lives at the root of the output directory

**Gitignore:** Users should add `.dox/` to `.gitignore`. `dox init` will suggest this.

**Codana mapping:** Each source subdirectory maps directly to a Codana document collection:
```bash
codana documents add-collection goreleaser .dox/goreleaser/
codana documents add-collection hono .dox/hono/
```

---

## 9. Dependencies

**Direct dependencies to add to `go.mod`:**

| Package                                  | Purpose               |
|------------------------------------------|-----------------------|
| `github.com/urfave/cli/v3`              | CLI framework         |
| `resty.dev/v3`                           | HTTP client (v3 vanity URL) |
| `github.com/knadh/koanf/v2`             | Config loading        |
| `github.com/knadh/koanf/parsers/toml/v2`| TOML parser           |
| `github.com/knadh/koanf/providers/file` | File provider         |
| `github.com/jedib0t/go-pretty/v6`       | Progress bars, tables |
| `github.com/samber/oops`                | Structured errors     |
| `github.com/bmatcuk/doublestar/v4`      | Glob matching         |
| `golang.org/x/sync`                     | errgroup              |

> **Note on resty**: Resty v3 uses the vanity import path `resty.dev/v3` (not the old `github.com/go-resty/resty/v2`). The docs in `.docs/resty/` cover the v3 API.

> **Note on go.mod**: The current `go.mod` has only indirect dependencies pulled in by the `tool` declarations (goreleaser, lefthook, task, goimports). When we add the direct dependencies above and run `go mod tidy`, many of these indirect deps will be retained (they're needed by the tool binaries) but our direct deps will be clearly separated.

---

## 10. Testing Strategy

### Unit Tests

Each `internal/` package gets a `_test.go` companion:

| Package            | Test File              | What's Tested                                                     |
|--------------------|------------------------|-------------------------------------------------------------------|
| `internal/config`  | `config_test.go`       | TOML parsing, defaults applied, validation errors, directory walk, `OutputDir` resolution |
| `internal/lockfile`| `lockfile_test.go`     | Read/write round-trip, atomic save, missing file returns empty, entry CRUD |
| `internal/source`  | `source_test.go`       | Factory returns correct types, unknown type error                 |
| `internal/source`  | `github_test.go`       | Pattern matching logic, single-file detection, tree diffing, file map building |
| `internal/source`  | `url_test.go`          | Filename resolution, conditional request header construction      |
| `internal/ui`      | `table_test.go`        | JSON output format                                                |

### HTTP Mocking

For source tests that involve HTTP calls, use `net/http/httptest.Server` to create local test servers that return canned GitHub API responses (tree listings, raw file content) and URL responses (with ETag/Last-Modified headers, 304 responses). This avoids hitting real APIs in tests and keeps tests fast and deterministic.

### Integration Tests (Phase 8)

Manual or scripted tests against real public repos to validate end-to-end:
- Sync a real GitHub docs directory (e.g., goreleaser)
- Sync a real URL source (e.g., hono llms-full.txt)
- Re-sync to verify freshness checking works (second run skips)
- Force sync to verify `--force` re-downloads
- Clean and re-sync

### Test Infrastructure

- Tests use `t.TempDir()` for isolated output directories
- The existing `task test` command (`go test -v -race -cover ./...`) runs all tests
- Pre-commit hook via lefthook already runs `task test` — no changes needed

---

## 11. Project File Updates

Before implementation begins, these existing files need updates:

### `.gitignore` — add:
```
.dox/
```

### `LICENSE` — create:
The goreleaser config references a LICENSE file (included in release archives) and declares the license as MIT. A `LICENSE` file must be created in the repo root.

### `README.md` — rewrite:
Currently just contains the project name. Should be updated with:
- One-line description
- Installation (`go install` / homebrew)
- Quick start (`dox init` → edit config → `dox sync`)
- Config reference (or link to it)
- Link to Codana for the indexing side

### Naming Fixes

Fix the naming inconsistencies left over from the llxt → dox rename:

| File             | Current                                | Change to                       |
|------------------|----------------------------------------|---------------------------------|
| `Taskfile.yml`   | `BINARY_NAME: llxt`, `./cmd/llxt`      | `BINARY_NAME: dox`, `./cmd/dox` |
| `.golangci.yml`  | `local-prefixes: github.com/g5becks/llxt` | `local-prefixes: github.com/g5becks/dox` |

---

## 12. Implementation Order

### Phase 1: Foundation
1. Fix naming inconsistencies in Taskfile.yml and .golangci.yml
2. Add `.dox/` to `.gitignore`, create `LICENSE` (MIT)
3. Create directory structure (`cmd/dox/`, `internal/config/`, `internal/source/`, `internal/lockfile/`, `internal/sync/`, `internal/ui/`)
4. Add direct dependencies to `go.mod` (`go get resty.dev/v3 github.com/urfave/cli/v3 ...` then `go mod tidy`)
5. Implement `cmd/dox/main.go` with CLI skeleton (all commands defined, actions return "not implemented" error)

### Phase 2: Config
6. Implement `internal/config/types.go` — struct definitions, defaults, validation
7. Implement `internal/config/config.go` — file discovery (directory walk), loading with koanf, unmarshalling
8. Write `internal/config/config_test.go` — test parsing, defaults, validation, directory walk

### Phase 3: Lock File
9. Implement `internal/lockfile/lockfile.go` — read/write/update operations
10. Write `internal/lockfile/lockfile_test.go` — round-trip, atomic save, missing file handling

### Phase 4: Sources
11. Implement `internal/source/source.go` — interface definition, factory function
12. Implement `internal/source/url.go` — URL source with conditional requests
13. Implement `internal/source/github.go` — GitHub source (Trees API, pattern matching, smart diff, raw downloads)
14. Write `internal/source/github_test.go` — pattern matching, single-file detection, tree diffing (using httptest)
15. Write `internal/source/url_test.go` — filename resolution, conditional requests, 304 handling (using httptest)

### Phase 5: UI
16. Implement `internal/ui/progress.go` — progress writer wrapper
17. Implement `internal/ui/table.go` — table rendering + JSON output for list

### Phase 6: Sync Orchestrator
18. Implement `internal/sync/sync.go` — worker pool, lock file integration, signal handling, summary reporting

### Phase 7: Wire Up Commands
19. Wire `sync` command — config → lock file → sync orchestrator → save lock
20. Wire `list` command — config → lock file → table rendering
21. Wire `clean` command — config → remove directories + lock entries
22. Wire `init` command — write template config to CWD
23. Wire `add` command — parse flags → validate → append to config file

### Phase 8: Polish
24. Dry-run mode implementation
25. Rate limit detection and user-facing warnings
26. Error summary at end of sync (which sources failed, which skipped, which synced)
27. Update README.md with usage documentation
28. Integration test with real repos (goreleaser, TanStack, hono llms.txt, etc.)

---

## 13. Config File Templates

### `dox init` output:

```toml
# dox.toml - Documentation source configuration
# Docs: https://github.com/g5becks/dox

# Directory where docs will be downloaded (relative to this file)
# Default: ".dox"
# output = ".dox"

# GitHub token for higher API rate limits (5000/hr vs 60/hr unauthenticated)
# Can also be set via GITHUB_TOKEN or GH_TOKEN environment variable
# github_token = ""

# --- Example: Download docs from a GitHub repo directory ---
# [sources.my-library]
# type = "github"
# repo = "owner/repo"
# path = "docs"
# ref = "main"                                       # optional (default: repo default branch)
# patterns = ["**/*.md", "**/*.mdx", "**/*.txt"]     # optional (these are the defaults)
# exclude = ["**/changelog.md"]                       # optional
# out = "custom-dir-name"                             # optional (default: source key name)

# --- Example: Download a single file from a URL ---
# [sources.my-framework]
# type = "url"
# url = "https://example.com/llms-full.txt"
# filename = "my-framework.txt"                       # optional (default: basename from URL)
```

---

## 14. Codana Integration Workflow

The intended end-to-end workflow:

```bash
# 1. Clone repo on new machine
git clone https://github.com/you/your-project.git
cd your-project

# 2. Download docs
dox sync

# 3. Index for Codana
codana documents add-collection goreleaser .dox/goreleaser/
codana documents add-collection hono .dox/hono/
codana documents index

# 4. AI agents can now search
codana mcp search_documents query:"goreleaser configuration" collection:goreleaser
```

This could be automated via a Taskfile task:

```yaml
# Taskfile.yml
tasks:
  setup:docs:
    desc: Download and index documentation
    cmds:
      - dox sync
      - codana documents add-collection goreleaser .dox/goreleaser/
      - codana documents add-collection hono .dox/hono/
      - codana documents index
```

---

## 15. Future Enhancements (Out of Scope for v1)

- **GitLab source type**: `type = "gitlab"` with `host` for self-hosted instances. Uses GitLab Repository Tree API (`GET /api/v4/projects/:id/repository/tree?recursive=true`)
- **Gitea/Codeberg/Forgejo source type**: `type = "gitea"` with `host`. Uses Gitea Git Trees API (same shape as GitHub)
- **llms.txt registry lookup**: Search https://directory.llmstxt.cloud/ or https://llmstxt.site/ by name, auto-add as URL source
- **MCP server mode**: `dox serve` exposes tools for AI agents to trigger syncs and manage sources
- **GitHub Enterprise**: `host = "github.example.com"` on GitHub sources
- **Zip/tarball sources**: Download and extract archive files
- **Auto-Codana integration**: `dox sync --index` runs `codana documents add-collection` + `codana documents index` after sync
- **Watch mode**: Re-sync on a schedule or file change
- **`dox remove <name>`**: Remove a source from config + clean its files
