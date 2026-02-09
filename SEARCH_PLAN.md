# Execution Spec: `dox search`

## 1. Goal

Add `dox search` so users and agents can discover docs without guessing file paths.

This command must support two modes:

- Metadata mode (default): fuzzy match manifest metadata.
- Content mode (`--content`): grep synced file contents.

## 2. Scope and Non-Goals

In scope:

- New CLI command: `dox search`.
- New `internal/search` package with metadata + content search.
- Table/JSON/CSV output parity with existing query commands.
- Tests and benchmarks.
- README docs update.

Out of scope:

- Full-text index persistence on disk.
- Concurrent file scanning.
- Highlighted terminal output.

## 3. Constraints from Current Codebase

The implementation must match existing patterns:

- CLI framework: `urfave/cli/v3` (`cmd/dox/*.go`).
- Error style: `samber/oops` with `.Code(...)` + `.Hint(...)`.
- Manifest source: `manifest.Load(cfg.Output)` where `cfg.Output` is already absolute.
- File path for synced docs: `filepath.Join(cfg.Output, collection.Dir, file.Path)` (must use `collection.Dir`, not collection name).
- Table output: `go-pretty/v6` with `table.StyleRounded`.
- Description truncation behavior: reuse `truncateDescription` in `cmd/dox/files.go`.
- Binary detection: `parser.IsBinary`.

## 4. CLI Contract

### Command syntax

```bash
dox search <query>
dox search <query> --collection <name>
dox search <query> --content
dox search <query> --content --regex
dox search <query> --json
dox search <query> --format csv
dox search <query> --limit 20
dox search <query> --desc-length 120
```

### Flags

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--config`, `-c` | string | auto-discover | Same behavior as existing query commands |
| `--collection` | string | empty (all) | Restrict search scope |
| `--content` | bool | false | Switch from metadata mode to file-content mode |
| `--regex` | bool | false | Only valid with `--content` |
| `--json` | bool | false | Shorthand for `--format json` |
| `--format` | string | `cfg.Display.Format` | `table`, `json`, `csv` |
| `--limit` | int | `cfg.Display.DefaultLimit` | If explicitly set to `0`, means unlimited |
| `--desc-length` | int | `cfg.Display.DescriptionLength` | Table-only truncation for long description/line text |

### Validation rules

- Missing/blank query: `INVALID_ARGS` with usage hint.
- `--regex` without `--content`: `INVALID_ARGS` with hint.
- Unknown collection: `COLLECTION_NOT_FOUND` with hint.
- Invalid regex: `SEARCH_ERROR` with hint.

## 5. Output Contract

### Metadata result shape

```go
type MetadataResult struct {
    Collection   string `json:"collection"`
    Path         string `json:"path"`
    Type         string `json:"type"`
    Description  string `json:"description,omitempty"`
    MatchField   string `json:"match_field"`   // path|description|heading|export
    MatchValue   string `json:"match_value"`   // exact field value that matched
    Score        int    `json:"score"`
}
```

Table columns:

- `COLLECTION`
- `PATH`
- `TYPE`
- `MATCH FIELD`
- `SCORE`
- `DESCRIPTION`

CSV header:

`collection,path,type,match_field,match_value,score,description`

### Content result shape

```go
type ContentResult struct {
    Collection string `json:"collection"`
    Path       string `json:"path"`
    Line       int    `json:"line"`
    Text       string `json:"text"`
}
```

Table columns:

- `COLLECTION`
- `PATH`
- `LINE`
- `TEXT`

CSV header:

`collection,path,line,text`

## 6. Internal Package Design

Create `internal/search` with two files.

### `internal/search/search.go` (metadata mode)

Concrete API:

```go
package search

import "github.com/g5becks/dox/internal/manifest"

type MetadataOptions struct {
    Query      string
    Collection string
    Limit      int
}

func Metadata(m *manifest.Manifest, opts MetadataOptions) ([]MetadataResult, error)
```

Internal index model:

```go
type indexEntry struct {
    Collection  string
    Path        string
    Type        string
    Description string
    MatchField  string
    MatchValue  string
}

type searchIndex struct {
    entries []indexEntry
}

func (s searchIndex) String(i int) string { return s.entries[i].MatchValue }
func (s searchIndex) Len() int            { return len(s.entries) }
```

Algorithm:

1. Validate query and collection.
2. Build flat index entries per file:
3. Add path entry (always).
4. Add description entry (if non-empty).
5. Add heading entries from `file.Outline.Headings`.
6. Add export entries from `file.Outline.Exports`.
7. Call `fuzzy.FindFrom(opts.Query, index)`.
8. Map fuzzy matches back to `indexEntry`.
9. Dedupe by `(collection,path)` keeping highest `Score` result.
10. Keep fuzzy ordering stable for equal score.
11. Apply `Limit` at the end (`0` = unlimited).

Dependency:

```bash
go get github.com/sahilm/fuzzy@latest
```

### `internal/search/content.go` (content mode)

Concrete API:

```go
package search

import "github.com/g5becks/dox/internal/manifest"

type ContentOptions struct {
    OutputDir   string
    Query       string
    Collection  string
    UseRegex    bool
    Limit       int
}

func Content(m *manifest.Manifest, opts ContentOptions) ([]ContentResult, error)
```

Implementation details:

- Iterate collections in sorted order for deterministic output.
- For each file, construct full path via `collection.Dir`.
- Skip missing/unreadable files (continue).
- Skip files larger than `50 * 1024 * 1024`.
- Read file bytes once, skip binary files with `parser.IsBinary`.
- Split lines, remove phantom trailing empty line.
- Literal mode: case-insensitive `strings.Contains`.
- Regex mode: compile once with case-insensitive prefix `(?i)`.
- Emit one `ContentResult` per matching line, line numbers are 1-based.
- Stop early once `Limit` reached (when `Limit > 0`).

## 7. CLI Wiring Design

Create `cmd/dox/search.go`.

Command factory:

```go
func newSearchCommand() *cli.Command {
    return &cli.Command{
        Name:      "search",
        Usage:     "Search documentation metadata or file content",
        ArgsUsage: "<query>",
        Flags: []cli.Flag{
            &cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Path to config file"},
            &cli.StringFlag{Name: "collection", Usage: "Search only within one collection"},
            &cli.BoolFlag{Name: "content", Usage: "Search file contents instead of metadata"},
            &cli.BoolFlag{Name: "regex", Usage: "Treat query as regex (requires --content)"},
            &cli.BoolFlag{Name: "json", Usage: "Output as JSON"},
            &cli.StringFlag{Name: "format", Usage: "Output format: table, json, csv"},
            &cli.IntFlag{Name: "limit", Usage: "Max results (0 = unlimited)"},
            &cli.IntFlag{Name: "desc-length", Usage: "Max table text length (0 = use config default)"},
        },
        Action: searchAction,
    }
}
```

Action flow:

1. Validate exactly 1 arg and non-empty query.
2. Validate `--regex`/`--content` dependency.
3. Load config + manifest.
4. Validate optional collection exists.
5. Resolve output format (`--json` overrides `--format`).
6. Resolve result limit.
7. Dispatch to metadata or content search.
8. Render table/json/csv.

Keep complexity low by splitting:

- `runMetadataSearch(...)`
- `runContentSearch(...)`
- `outputMetadataTable/JSON/CSV`
- `outputContentTable/JSON/CSV`

Register command in `cmd/dox/main.go`:

```go
newOutlineCommand(),
newSearchCommand(),
```

## 8. Edge Case Behavior (Required)

| Case | Required behavior |
|------|-------------------|
| Query is empty/whitespace | return `INVALID_ARGS` |
| No matches | render empty result set (headers only for table, `[]` for JSON, header-only CSV) |
| Regex invalid | return `SEARCH_ERROR` |
| Binary file in content mode | skip silently |
| File >50MB | skip silently |
| File missing on disk | skip silently |
| Collection filter is unknown | `COLLECTION_NOT_FOUND` |
| `--regex` without `--content` | `INVALID_ARGS` |

## 9. Concrete Testing Plan

Automated tests are required before merge.

### A. `internal/search/search_test.go` (metadata)

Use table-driven tests and `t.Parallel()`.

Required tests:

- `TestMetadata_PathMatch`
- `TestMetadata_DescriptionMatch`
- `TestMetadata_HeadingMatch`
- `TestMetadata_ExportMatch`
- `TestMetadata_CollectionFilter`
- `TestMetadata_UnknownCollection`
- `TestMetadata_Limit`
- `TestMetadata_DedupesBestScorePerFile`
- `TestMetadata_EmptyManifest`
- `TestMetadata_EmptyQuery`

Assertions must verify:

- Result count.
- Deterministic ordering.
- `MatchField` correctness.
- `Score` is non-increasing across results.

### B. `internal/search/content_test.go` (content)

Use `t.TempDir()` and real files.

Required tests:

- `TestContent_LiteralCaseInsensitive`
- `TestContent_RegexCaseInsensitive`
- `TestContent_InvalidRegex`
- `TestContent_LimitStopsEarly`
- `TestContent_CollectionFilter`
- `TestContent_UnknownCollection`
- `TestContent_SkipsBinary`
- `TestContent_SkipsMissingFiles`
- `TestContent_SkipsLargeFiles`
- `TestContent_LineNumbersOneBased`

### C. `internal/search/bench_test.go`

Benchmarks:

- `BenchmarkBuildIndex700Files`
- `BenchmarkMetadataSearch700Files`
- `BenchmarkContentSearch700Files`

Implementation style must match existing benchmark pattern (`b.Loop()`), as used in `internal/manifest/bench_test.go`.

### D. CLI smoke tests (manual, required in PR checklist)

Run against local synced docs:

```bash
dox search config
dox search config --collection goreleaser
dox search install --json
dox search install --format csv
dox search install --limit 5
dox search "installation" --content
dox search "func.*Logger" --content --regex
dox search missing-term
dox search "x" --regex
dox search ""            # via shell quoting
```

Expected outcomes must be documented in PR notes, especially error code/hint behavior.

## 10. File Change List

New files:

- `internal/search/search.go`
- `internal/search/content.go`
- `internal/search/search_test.go`
- `internal/search/content_test.go`
- `internal/search/bench_test.go`
- `cmd/dox/search.go`

Modified files:

- `cmd/dox/main.go` (register command)
- `go.mod`, `go.sum` (add `github.com/sahilm/fuzzy`)
- `README.md` (new Search section + AI workflow update)

## 11. Implementation Sequence

1. Add dependency (`go get github.com/sahilm/fuzzy@latest`).
2. Implement metadata search package (`search.go`).
3. Implement content search package (`content.go`).
4. Add metadata/content unit tests.
5. Add benchmarks.
6. Implement CLI command in `cmd/dox/search.go`.
7. Register command in `cmd/dox/main.go`.
8. Update README.
9. Run full verification.

## 12. Verification Gates (Must Pass)

```bash
go build ./...
go test -race ./...
go tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint run
```

Definition of done:

- All verification gates pass.
- New tests cover both modes and error paths.
- Manual smoke matrix completed.
- README includes search examples and flags.
