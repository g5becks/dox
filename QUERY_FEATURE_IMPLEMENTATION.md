# dox Query Feature Implementation Plan

## Overview

Add CLI query commands to dox to enable AI agents to discover and read synced documentation without external tools. This replaces the need for MCP servers like context7 by providing direct CLI access to documentation collections.

---

## Goals

1. **Discovery**: List available collections (docsets) and their files
2. **Navigation**: Browse file structures and metadata
3. **Reading**: Access file contents with pagination and formatting
4. **Structure**: View document outlines (headings, structure)
5. **AI-Friendly**: JSON output, familiar CLI patterns, transferable knowledge from tools like `gh` CLI

---

## Supported File Types

| Type | Extension | Description Extraction | Outline Support |
|------|-----------|------------------------|-----------------|
| Markdown | `.md` | Frontmatter `title`/`description` → First heading → First paragraph | Headings (h1-h6) |
| MDX | `.mdx` | Frontmatter `title`/`description` → First heading → First paragraph | Headings (h1-h6) |
| Text | `.txt` | First non-empty line | None (no structure) |
| TypeScript/TSX | `.tsx`, `.ts` | **Documentation:** First JSX heading (`<h1>`) or paragraph (`<p>`)<br>**Code:** JSDoc comment or first comment block | **Documentation:** JSX headings (h1-h6)<br>**Code:** Exports, functions, components |

---

## Error Handling Strategy

dox uses `github.com/samber/oops` for structured error handling. All new query commands must follow this pattern for consistency.

### Error Codes

```go
const (
    // Manifest errors
    ErrManifestNotFound     = "MANIFEST_NOT_FOUND"
    ErrManifestCorrupted    = "MANIFEST_CORRUPTED"
    ErrManifestVersion      = "MANIFEST_VERSION_MISMATCH"

    // Collection errors
    ErrCollectionNotFound   = "COLLECTION_NOT_FOUND"
    ErrCollectionEmpty      = "COLLECTION_EMPTY"

    // File errors
    ErrFileNotFound         = "FILE_NOT_FOUND"
    ErrFileNotReadable      = "FILE_NOT_READABLE"
    ErrFileTooLarge         = "FILE_TOO_LARGE"
    ErrFileBinary           = "FILE_BINARY"

    // Query errors
    ErrInvalidField         = "INVALID_FIELD"
    ErrInvalidFormat        = "INVALID_FORMAT"
    ErrInvalidOffset        = "INVALID_OFFSET"
)
```

### Error Messages with Hints

```go
// Example: Manifest not found
return oops.
    Code(ErrManifestNotFound).
    Hint("Run 'dox sync' to generate the manifest first").
    Wrapf(err, "failed to load manifest")

// Example: Collection not found
return oops.
    Code(ErrCollectionNotFound).
    With("collection", collectionName).
    Hint("Run 'dox collections' to see available collections").
    Errorf("collection %q not found", collectionName)
```

### Error Handling Decisions

| Scenario | Behavior | Exit Code |
|----------|----------|-----------|
| Manifest missing | Error with hint to run `dox sync` | 1 |
| Manifest corrupted | Error with hint to delete `.dox/manifest.json` and resync | 1 |
| Collection not found | Error listing available collections | 1 |
| File not found | Error with file path | 1 |
| Binary file in listing | Skip with warning logged | 0 |
| Large file (>50MB) | Include in manifest with size warning, skip parsing | 0 |
| Parser failure on one file | Log warning, continue with other files | 0 |
| Invalid CLI flag value | Error with usage help | 2 |

---

## Type Definitions

### Manifest Structure

```go
// internal/manifest/types.go

type Manifest struct {
    Version    string                 `json:"version"`
    Generated  time.Time              `json:"generated"`
    Collections map[string]*Collection `json:"collections"`
}

type Collection struct {
    Name      string     `json:"name"`
    Type      string     `json:"type"`      // "github", "url", etc.
    Source    string     `json:"source"`    // repo or URL
    Path      string     `json:"path"`      // optional path in repo
    Ref       string     `json:"ref"`       // branch/tag/commit
    LastSync  time.Time  `json:"last_sync"`
    FileCount int        `json:"file_count"`
    TotalSize int64      `json:"total_size"`
    Files     []FileInfo `json:"files"`
}

type FileInfo struct {
    Path          string        `json:"path"`
    Type          string        `json:"type"`                      // "md", "mdx", "txt", "tsx", "ts"
    Size          int64         `json:"size"`
    Lines         int           `json:"lines"`
    Modified      time.Time     `json:"modified"`
    Description   string        `json:"description"`
    ComponentType ComponentType `json:"component_type,omitempty"`  // only for tsx/ts files
    Warning       string        `json:"warning,omitempty"`         // e.g., "file_too_large", "binary_skipped"
    Outline       *Outline      `json:"outline,omitempty"`
}

type Outline struct {
    Type     OutlineType `json:"type"`
    Headings []Heading   `json:"headings,omitempty"` // for md, mdx, tsx-docs
    Exports  []Export    `json:"exports,omitempty"`  // for tsx-code, ts
}

type OutlineType string

const (
    OutlineTypeHeadings OutlineType = "headings"
    OutlineTypeExports  OutlineType = "exports"
    OutlineTypeNone     OutlineType = "none"
)

type Heading struct {
    Level int    `json:"level"` // 1-6
    Text  string `json:"text"`
    Line  int    `json:"line"`
}

type Export struct {
    Type string `json:"type"` // "interface", "const", "function", "class"
    Name string `json:"name"`
    Line int    `json:"line"`
}
```

### Parser Interface

```go
// internal/parser/parser.go

type Parser interface {
    // Parse extracts description and outline from file content
    Parse(path string, content []byte) (*ParseResult, error)

    // CanParse returns true if this parser can handle the file
    CanParse(path string) bool
}

type ParseResult struct {
    Description string
    Outline     *Outline
    Lines       int
}

// Component type for TSX files
type ComponentType string

const (
    ComponentTypeDocumentation ComponentType = "documentation"
    ComponentTypeCode          ComponentType = "code"
)
```

---

## Edge Cases & Handling

### File Size & Content Issues

| Edge Case | Detection | Handling |
|-----------|-----------|----------|
| Binary files | Check first 512 bytes for null bytes, validate UTF-8 | Skip during manifest generation, log warning |
| Very large files (>50MB) | Check file size before reading | Add to manifest with size info, skip parsing, add warning field |
| Files >100k lines | Count after reading | Parse normally but log performance warning |
| Empty files | File size = 0 or all whitespace | Include with description: "(empty file)" |
| Invalid UTF-8 | UTF-8 validation fails | Skip with warning, or include with description: "(encoding error)" |
| BOM (Byte Order Mark) | Check first 3 bytes for UTF-8 BOM | Strip BOM before parsing |

### Malformed Content

| Edge Case | Detection | Handling |
|-----------|-----------|----------|
| Corrupted markdown | Parser error | Include file with description: "(parse error)", empty outline |
| Mismatched JSX tags | Regex fails to match | Fall back to code component detection |
| Missing closing tags | Incomplete heading extraction | Use partial results |
| Code-only markdown | No headings found | Use first paragraph or first line as description |
| TSX with only imports | No headings, no exports | Description: filename, outline: empty |

### Manifest Issues

| Edge Case | Detection | Handling |
|-----------|-----------|----------|
| Manifest missing | File doesn't exist | Error with hint: "Run 'dox sync'" |
| Manifest corrupted (invalid JSON) | JSON unmarshal error | Error with hint: "Delete .dox/manifest.json and run 'dox sync'" |
| Manifest version mismatch | Version != "1.0.0" | Warning, attempt to read anyway, regenerate on next sync |
| Partial write (power failure) | JSON parse error or incomplete | Same as corrupted |
| Stale manifest (collection deleted) | Collection in manifest but not in config | Display with warning in `dox collections` |
| File deleted from disk | File referenced in manifest doesn't exist | `dox cat` errors gracefully |

### Path & Filesystem Issues

| Edge Case | Detection | Handling |
|-----------|-----------|----------|
| Very long paths (>260 chars) | Path length check | Truncate with ellipsis in table display, full path in JSON |
| Special chars in filenames | Quotes, newlines, null bytes | Escape in JSON output, sanitize in table display |
| Symbolic links | Check `os.Lstat()` | Follow symlinks, same as regular files |
| Non-existent collection | Collection name not in manifest | Error listing available collections |
| Empty collection (zero files) | FileCount = 0 | Display message: "No files found in collection" |

### Concurrent Access

| Scenario | Handling |
|----------|----------|
| Multiple `dox files` processes | Read-only, safe for concurrent access |
| `dox sync` while `dox cat` running | Manifest may be inconsistent, acceptable (eventual consistency) |
| Manifest being written during read | Use atomic file write (write temp, rename) |

---

## CLI Commands

### 1. `dox collections`

List all available documentation collections with metadata from the manifest.

**Note:** This is distinct from the existing `dox list` command. `dox list` shows configured sources and sync status from `dox.toml`. `dox collections` shows synced collections with file counts, sizes, and metadata from the generated manifest. Both commands serve different purposes and coexist.

**Usage:**
```bash
dox collections [options]
```

**Options:**
- `--json` - Output as JSON
- `--limit N` - Show first N collections (default: all)

**Table Output:**
```
NAME         TYPE    FILES  SIZE     LAST SYNC
goreleaser   github    142  2.1 MB   2024-02-08 10:30
hono         url         1  156 KB   2024-02-08 10:30
solidjs      github     89  1.8 MB   2024-02-08 10:29
```

**JSON Output:**
```json
[
  {
    "name": "goreleaser",
    "type": "github",
    "source": "goreleaser/goreleaser",
    "path": "www/docs",
    "files": 142,
    "size": 2201600,
    "last_sync": "2024-02-08T10:30:00Z"
  }
]
```

---

### 2. `dox files <collection>`

List files in a collection with metadata.

**Note:** The command is named `files` (not `list`) because `dox list` already exists and shows configured sources and sync status. `dox files` is the new command for browsing files within a specific collection.

**Usage:**
```bash
dox files <collection> [options]
```

**Options:**
- `--json` - Output as JSON
- `--limit N` - Show first N files (default: from config or 50)
- `--all` - Show all files (no limit)
- `--format string` - Output format: `table`, `json`, `csv` (default: `table`)
- `--fields string` - Comma-separated fields (default: `path,type,lines,size,description`)
  - Available fields: `path`, `type`, `lines`, `size`, `description`, `modified`
- `--desc-length N` - Max description length (default: from config or 200)

**Table Output:**
```
PATH                          TYPE  LINES  SIZE    DESCRIPTION
docs/install.md              md      152  4.5 KB  Installation and Setup - Learn how to install GoReleaser using package managers
docs/customization/index.md  md      203  8.2 KB  How to customize GoReleaser builds - Configure builds, archives, and release
api/config.md                md      891  42 KB   Configuration reference for .goreleaser.yaml - Complete API documentation
app/cdp-mode.tsx             tsx     120  3.8 KB  CDP Mode - Connect to an existing browser via Chrome DevTools Protocol
components/Button.tsx        tsx      45  1.2 KB  Button component with variants and sizes
utils/helpers.ts             ts       89  2.3 KB  Utility functions for data transformation

(showing 50 of 142 files, use --all to show all or --limit N)
```

**JSON Output:**
```json
[
  {
    "path": "docs/install.md",
    "type": "md",
    "lines": 152,
    "size": 4608,
    "description": "Installation and Setup - Learn how to install GoReleaser...",
    "modified": "2024-02-08T10:30:00Z"
  }
]
```

---

### 3. `dox cat <collection> <file>`

Read file contents.

**Usage:**
```bash
dox cat <collection> <file> [options]
```

**Options:**
- `--json` - Output as JSON (includes metadata + content)
- `--no-line-numbers` - Don't show line numbers (default: show)
- `--offset N` - Start at line N (default: 0)
- `--limit N` - Show N lines (default: all)

**Default Output (with line numbers):**
```
     1  # Installation
     2
     3  GoReleaser can be installed using various package managers.
     4
     5  ## Package Managers
     6
     7  ### Homebrew
```

**JSON Output:**
```json
{
  "collection": "goreleaser",
  "path": "docs/install.md",
  "type": "md",
  "lines": 152,
  "size": 4608,
  "content": "# Installation\n\nGoReleaser can be...",
  "offset": 0,
  "limit": 152
}
```

---

### 4. `dox outline <collection> <file>`

Show file structure/outline.

**Usage:**
```bash
dox outline <collection> <file> [options]
```

**Options:**
- `--json` - Output as JSON

**Markdown/MDX Output:**
```
docs/install.md (152 lines, 4.5 KB)

STRUCTURE:
  1  # Installation
  8    ## Package Managers
 12      ### Homebrew
 18      ### Apt
 25      ### Scoop (Windows)
 30    ## From Source
 35      ### Prerequisites
 45      ### Build Steps
```

**TSX Output (Documentation Component):**
```
app/cdp-mode.tsx (120 lines, 3.8 KB)

STRUCTURE:
  7  # CDP Mode
 11    ## Remote WebSocket URLs
 14    ## Use cases
 20    ## Global options
 45    ## Cloud providers
```

**TSX Output (Code Component):**
```
components/Button.tsx (45 lines, 1.2 KB)

EXPORTS:
  5   interface ButtonProps
  15  export const Button: React.FC<ButtonProps>
  30  export const IconButton: React.FC<IconButtonProps>
```

**Text File Output:**
```
README.txt (89 lines, 2.1 KB)

No outline available for text files.
Use 'dox cat' to read the full content.
```

**JSON Output (Markdown):**
```json
{
  "collection": "goreleaser",
  "path": "docs/install.md",
  "lines": 152,
  "size": 4608,
  "type": "md",
  "headings": [
    {"level": 1, "text": "Installation", "line": 1},
    {"level": 2, "text": "Package Managers", "line": 8},
    {"level": 3, "text": "Homebrew", "line": 12}
  ]
}
```

**JSON Output (TSX - Documentation Component):**
```json
{
  "collection": "agent-browser",
  "path": "app/cdp-mode.tsx",
  "lines": 120,
  "size": 3891,
  "type": "tsx",
  "component_type": "documentation",
  "headings": [
    {"level": 1, "text": "CDP Mode", "line": 7},
    {"level": 2, "text": "Remote WebSocket URLs", "line": 11},
    {"level": 2, "text": "Use cases", "line": 14}
  ]
}
```

**JSON Output (TSX - Code Component):**
```json
{
  "collection": "solidjs",
  "path": "components/Button.tsx",
  "lines": 45,
  "size": 1234,
  "type": "tsx",
  "component_type": "code",
  "exports": [
    {"type": "interface", "name": "ButtonProps", "line": 5},
    {"type": "const", "name": "Button", "line": 15}
  ]
}
```

---

## Configuration (`dox.toml`)

Add new `[display]` section to `dox.toml`:

```toml
# Existing config...
output = ".dox"
max_parallel = 20

[display]
# Default number of files to show in 'dox files' (0 = all)
default_limit = 50

# Maximum length for file descriptions
description_length = 200

# Show line numbers by default in 'dox cat'
line_numbers = true

# Default output format: table, json, csv
format = "table"

# Default fields to show in 'dox files' table
list_fields = ["path", "type", "lines", "size", "description"]

# Existing sources...
[sources.goreleaser]
repo = "goreleaser/goreleaser"
path = "www/docs"
```

**Configuration Precedence:**
1. CLI flags (highest priority)
2. `dox.toml` `[display]` section
3. Built-in defaults (lowest priority)

---

## Manifest File (`.dox/manifest.json`)

Generated during `dox sync` to enable fast queries without re-reading files.

**Structure:**
```json
{
  "version": "1.0.0",
  "generated": "2024-02-08T10:30:00Z",
  "collections": {
    "goreleaser": {
      "name": "goreleaser",
      "type": "github",
      "source": "goreleaser/goreleaser",
      "path": "www/docs",
      "ref": "main",
      "last_sync": "2024-02-08T10:30:00Z",
      "file_count": 142,
      "total_size": 2201600,
      "files": [
        {
          "path": "docs/install.md",
          "type": "md",
          "size": 4608,
          "lines": 152,
          "modified": "2024-02-08T10:30:00Z",
          "description": "Installation and Setup - Learn how to install GoReleaser using package managers",
          "outline": {
            "type": "headings",
            "headings": [
              {"level": 1, "text": "Installation", "line": 1},
              {"level": 2, "text": "Package Managers", "line": 8}
            ]
          }
        },
        {
          "path": "app/cdp-mode.tsx",
          "type": "tsx",
          "size": 3891,
          "lines": 120,
          "modified": "2024-02-08T10:30:00Z",
          "description": "CDP Mode",
          "component_type": "documentation",
          "outline": {
            "type": "headings",
            "headings": [
              {"level": 1, "text": "CDP Mode", "line": 7},
              {"level": 2, "text": "Remote WebSocket URLs", "line": 11}
            ]
          }
        },
        {
          "path": "components/Button.tsx",
          "type": "tsx",
          "size": 1234,
          "lines": 45,
          "modified": "2024-02-08T10:30:00Z",
          "description": "Button component with variants and sizes",
          "component_type": "code",
          "outline": {
            "type": "exports",
            "exports": [
              {"type": "interface", "name": "ButtonProps", "line": 5},
              {"type": "const", "name": "Button", "line": 15}
            ]
          }
        }
      ]
    }
  }
}
```

**Location:** `.dox/manifest.json` (alongside synced files)

**Regeneration:** Generated on every `dox sync`

---

## File Type Handling

### Markdown (`.md`)

**Description Extraction (Priority Order):**
1. If frontmatter exists with `title` or `description` field → use that (common in static site generators like Hugo, Jekyll, Docusaurus)
2. First H1 heading text (`# Title`)
3. If heading is followed by a paragraph within 2 lines, append: `"Title - First paragraph text"`
4. If no H1, use first non-empty, non-code-block line
5. Truncate to `description_length` (default 200 chars)

**Outline Extraction:**
- Parse all headings (`#`, `##`, `###`, etc.)
- Record heading level, text, and line number
- Support both ATX (`# Heading`) and Setext styles
- Ignore headings inside code blocks (fenced with ``` or indented)

**Example:**
```markdown
# Installation

Learn how to install GoReleaser using package managers or build from source.

## Package Managers
```
- Description: `"Installation - Learn how to install GoReleaser using package managers or build from source."`
- Outline: `[{level: 1, text: "Installation", line: 1}, {level: 2, text: "Package Managers", line: 5}]`

---

### MDX (`.mdx`)

**Description Extraction (Priority Order):**
1. If frontmatter exists with `title` or `description` field → use that
2. Otherwise, same as Markdown (first heading + first paragraph)
3. Ignore JSX components and import statements in description extraction

**Outline Extraction:**
- Parse headings like Markdown
- Ignore import statements and JSX for basic outline
- Strip frontmatter block (`---` delimiters) before parsing headings

**Example (with frontmatter):**
```mdx
---
title: Using Buttons
description: Learn how to use Button components in your app
---

import { Button } from './components'

# Using Buttons

This guide shows how to use the Button component...

## Props
```
- Description: `"Using Buttons - Learn how to use Button components in your app"` (from frontmatter)
- Outline: `[{level: 1, text: "Using Buttons", line: 8}, {level: 2, text: "Props", line: 14}]`

**Example (without frontmatter):**
```mdx
import { Button } from './components'

# Using Buttons

This guide shows how to use the Button component...

## Props
```
- Description: `"Using Buttons - This guide shows how to use the Button component"` (from heading + paragraph)
- Outline: Same as Markdown (headings only)

---

### Text (`.txt`)

**Description Extraction:**
- First non-empty line
- Truncate to `description_length`

**Outline Extraction:**
- No structural outline (plain text)
- Return "No outline available" message
- Suggest using `dox cat` instead

**Example:**
```
This is a plain text documentation file.
It contains installation instructions.

Step 1: Download the binary
```
- Description: `"This is a plain text documentation file."`
- Outline: None

---

### TypeScript/TSX (`.tsx`, `.ts`)

**Strategy:** TSX files in documentation are typically React components that *render* documentation content via JSX. We need to extract the actual documentation structure from the JSX, not just the code exports.

**Detection - Documentation vs Code Component:**
1. **Documentation Component:** Contains JSX elements like `<h1>`, `<h2>`, `<p>`, `<div className="prose">`, etc.
2. **Code Component:** Pure logic, exports, interfaces - no significant JSX content

**Description Extraction (Priority Order):**
1. **If Documentation Component:**
   - First `<h1>` text content
   - If no `<h1>`, first `<p>` text content
   - Strip JSX tags, get plain text
   - Truncate to `description_length`

2. **If Code Component:**
   - First JSDoc comment block (`/** ... */`)
   - If no JSDoc, first regular comment (`// ...` or `/* ... */`)
   - If no comments, export name (e.g., "Button component")

**Outline Extraction:**
1. **If Documentation Component:**
   - Parse JSX heading elements: `<h1>`, `<h2>`, `<h3>`, etc.
   - Extract text content and line numbers
   - Build heading hierarchy (similar to markdown)
   - Ignore CodeBlock and other component tags

2. **If Code Component:**
   - Fall back to exports (interfaces, functions, components)
   - Record export type, name, and line number

**Example (Documentation Component):**
```tsx
import { CodeBlock } from "@/components/code-block";

export default function CDPMode() {
  return (
    <div className="max-w-2xl mx-auto px-4 sm:px-6 py-8 sm:py-12">
      <div className="prose">
        <h1>CDP Mode</h1>
        <p>Connect to an existing browser via Chrome DevTools Protocol:</p>

        <h2>Remote WebSocket URLs</h2>
        <p>Connect to remote browser services via WebSocket URL:</p>

        <h2>Use cases</h2>
        <p>This enables control of:</p>
        <ul>
          <li>Electron apps</li>
          <li>Chrome/Chromium with remote debugging</li>
        </ul>
      </div>
    </div>
  );
}
```
- **Description:** `"CDP Mode"`
- **Outline:**
  ```json
  [
    {"level": 1, "text": "CDP Mode", "line": 7},
    {"level": 2, "text": "Remote WebSocket URLs", "line": 11},
    {"level": 2, "text": "Use cases", "line": 14}
  ]
  ```

**Example (Code Component):**
```tsx
/**
 * Button component with variants and sizes
 */
interface ButtonProps {
  variant: 'primary' | 'secondary'
}

export const Button: React.FC<ButtonProps> = (props) => {
  return <button {...props} />
}
```
- **Description:** `"Button component with variants and sizes"` (from JSDoc)
- **Outline:**
  ```json
  [
    {"type": "interface", "name": "ButtonProps", "line": 4},
    {"type": "const", "name": "Button", "line": 8}
  ]
  ```

**Implementation Notes:**
- Use regex to detect JSX heading tags: `<h[1-6][^>]*>(.*?)</h[1-6]>`
- Strip nested tags to get plain text content
- If ≥2 heading tags with non-empty text content found → Documentation Component
- If <2 heading tags found → Code Component (fall back to exports)

---

## Implementation Phases

### Phase 1: Manifest Generation (Week 1)

**Goal:** Generate `.dox/manifest.json` during sync with metadata for all file types.

**Tasks:**

1. **Create manifest package** (`internal/manifest/`)
   - [ ] `types.go` - Manifest, Collection, FileInfo, Outline structs
   - [ ] `manifest.go` - Manifest loading, saving, versioning
   - [ ] `generator.go` - Generate manifest from synced files
   - [ ] `reader.go` - Query manifest (get collection, get file, list files)
   - [ ] `validator.go` - Validate manifest version, check corruption

2. **Create parser package** (`internal/parser/`)
   - [ ] `parser.go` - Parser interface and ParseResult type
   - [ ] `utils.go` - Binary detection, UTF-8 validation, BOM stripping
   - [ ] `markdown.go` - Parse `.md` files using `gomarkdown/markdown`
     - [ ] Strip frontmatter block (`---` delimiters) before parsing
     - [ ] If frontmatter has `title`/`description`, use for description
     - [ ] Extract first heading or first paragraph as fallback description
     - [ ] Parse heading hierarchy (h1-h6)
     - [ ] Handle both ATX (`#`) and Setext heading styles
     - [ ] Handle markdown with only code blocks (no headings)
   - [ ] `mdx.go` - Parse `.mdx` files
     - [ ] Strip frontmatter block (`---` delimiters) before markdown parsing
     - [ ] If frontmatter has `title`/`description`, use for description
     - [ ] Reuse markdown parser for heading extraction
     - [ ] Ignore JSX imports and components
   - [ ] `text.go` - Parse `.txt` files
     - [ ] Extract first non-empty line as description
     - [ ] Return OutlineTypeNone
   - [ ] `typescript.go` - Parse `.tsx`/`.ts` files
     - [ ] Detect documentation vs code component (≥2 JSX headings with content = documentation)
     - [ ] Extract JSX headings from documentation components (strip nested tags)
     - [ ] Extract exports from code components (interfaces, functions, consts)
     - [ ] Extract JSDoc/comments for descriptions
     - [ ] Handle files with only imports (no content)

3. **Binary file detection** (`internal/parser/utils.go`)
   - [ ] `isBinary(content []byte) bool` - Check first 512 bytes for null bytes
   - [ ] `isValidUTF8(content []byte) bool` - Validate UTF-8 encoding
   - [ ] `stripBOM(content []byte) []byte` - Remove UTF-8 BOM if present
   - [ ] Return error for binary files, skip in manifest with warning

4. **Large file handling**
   - [ ] Check file size before reading
   - [ ] Files >50MB: Add to manifest with size info, skip parsing, set description to "(large file - parsing skipped)"
   - [ ] Files >100k lines: Parse but log performance warning
   - [ ] Add `warning` field to FileInfo for large/binary file notices

5. **Integrate into sync** (`internal/sync/sync.go`)
   - [ ] Add manifest generation call after `lock.Save()` (line ~155)
   - [ ] Hook point:
     ```go
     // After lock.Save()
     if err := manifest.Generate(ctx, outputDir, cfg); err != nil {
         // Non-fatal: log warning, continue
         log.Warnf("Failed to generate manifest: %v", err)
     }
     ```
   - [ ] Generate manifest at `{outputDir}/manifest.json`
   - [ ] Use atomic write: write to temp file, rename to `manifest.json`
   - [ ] Set manifest version to "1.0.0"

6. **File type detection**
   - [ ] `DetectFileType(path string) string` in `internal/parser/utils.go`
   - [ ] Map extensions: `.md`, `.mdx`, `.txt`, `.tsx`, `.ts`
   - [ ] Return `"unknown"` for unrecognized types (skip parsing)

7. **Error handling**
   - [ ] Parser failure on single file: log warning, continue with other files
   - [ ] Manifest write failure: return error (fatal)
   - [ ] Use `oops` library for all errors with codes and hints

**Dependencies:**
- Markdown parser: `github.com/gomarkdown/markdown`
- TSX/TS parser: Regex-based extraction using stdlib `regexp` and `strings`
  - JSX heading detection: `<h[1-6][^>]*>(.*?)</h[1-6]>` (extract text, strip nested tags)
  - Export detection: `^\s*export\s+(const|function|interface|type|class)\s+(\w+)`
  - JSDoc extraction: `/\*\*\s*\n([^*]|\*(?!\/))*\*/`
  - Component type heuristic: If ≥2 `<h[1-6]>` tags with non-empty text → documentation

**Testing:**
- [ ] **Unit tests for parsers:**
  - [ ] `markdown_test.go`:
    - [ ] Markdown with frontmatter (title + description)
    - [ ] Markdown without frontmatter
    - [ ] ATX headings (`# H1`, `## H2`)
    - [ ] Setext headings (underlined with `===` or `---`)
    - [ ] Markdown with only code blocks
    - [ ] Empty markdown files
    - [ ] Markdown with escaped headings (`\# Not a heading`)
  - [ ] `mdx_test.go`:
    - [ ] MDX with frontmatter (title + description)
    - [ ] MDX with frontmatter (title only, no description)
    - [ ] MDX without frontmatter (falls back to markdown parsing)
    - [ ] MDX with import statements
    - [ ] MDX with embedded JSX components
    - [ ] MDX with both markdown and JSX
  - [ ] `text_test.go`:
    - [ ] Plain text files
    - [ ] Empty text files
    - [ ] Text with leading whitespace
  - [ ] `typescript_test.go`:
    - [ ] TSX documentation component (with JSX headings)
    - [ ] TSX code component (with exports)
    - [ ] TS file with exports
    - [ ] TSX with only imports (edge case)
    - [ ] TSX with nested JSX tags in headings
    - [ ] JSDoc comment extraction
  - [ ] `utils_test.go`:
    - [ ] Binary file detection (null bytes, images, PDFs)
    - [ ] UTF-8 validation (valid, invalid, mixed)
    - [ ] BOM stripping (UTF-8 BOM, no BOM)

- [ ] **Integration tests:**
  - [ ] `manifest_test.go`:
    - [ ] Sync repo → verify manifest exists
    - [ ] Verify manifest structure (version, collections, files)
    - [ ] Verify file metadata (size, lines, description)
    - [ ] Verify outline extraction for each file type
  - [ ] `large_collection_test.go`:
    - [ ] Generate manifest with 1000+ files
    - [ ] Measure generation time (<30s expected)

- [ ] **Edge case tests:**
  - [ ] Empty files (all types)
  - [ ] Binary files mixed with docs
  - [ ] Files with BOM
  - [ ] Invalid UTF-8 files
  - [ ] Very large files (>50MB)
  - [ ] Corrupted markdown (unclosed tags, invalid syntax)
  - [ ] TSX with mismatched JSX tags

- [ ] **Real-world tests:**
  - [ ] Sync goreleaser docs (md files)
  - [ ] Sync solidjs docs (mdx files)
  - [ ] Sync agent-browser docs (tsx documentation components)
  - [ ] Verify all files parsed correctly

---

### Phase 2: CLI Query Commands (Week 2)

**Goal:** Implement `collections`, `list`, `cat`, `outline` commands.

**Tasks:**

1. **Create query package** (`internal/query/`)
   - [ ] `query.go` - Query interface and manifest loader
   - [ ] `collections.go` - List collections from manifest
   - [ ] `files.go` - List files in collection with filters
   - [ ] `cat.go` - Read file contents with pagination
   - [ ] `outline.go` - Show file structure from manifest

2. **Add CLI commands** (`cmd/dox/`)
   - [ ] `collections.go` - `dox collections` command
   - [ ] `files.go` - `dox files <collection>` command
   - [ ] `cat.go` - `dox cat <collection> <file>` command
   - [ ] `outline.go` - `dox outline <collection> <file>` command

3. **Command registration**
   - [ ] Register commands in `main.go`
   - [ ] Set up flags and arguments
   - [ ] Add help text and examples

**Command Structure (using `urfave/cli/v3`):**
```go
// Note: dox uses newXxxCommand() factory functions (see existing newListCommand, newSyncCommand, etc.)

func newCollectionsCommand() *cli.Command {
    return &cli.Command{
        Name:  "collections",
        Usage: "List all documentation collections",
        Flags: []cli.Flag{
            &cli.BoolFlag{Name: "json", Usage: "Output as JSON"},
            &cli.IntFlag{Name: "limit", Usage: "Limit number of results"},
        },
        Action: runCollections,
    }
}

func newFilesCommand() *cli.Command {
    return &cli.Command{
        Name:      "files",
        Usage:     "List files in a collection",
        ArgsUsage: "<collection>",
        Flags: []cli.Flag{
            &cli.BoolFlag{Name: "json", Usage: "Output as JSON"},
            &cli.IntFlag{Name: "limit", Value: 50, Usage: "Show first N files"},
            &cli.BoolFlag{Name: "all", Usage: "Show all files (no limit)"},
            &cli.StringFlag{Name: "format", Value: "table", Usage: "Output format: table, json, csv"},
            &cli.StringFlag{Name: "fields", Usage: "Comma-separated fields to show"},
            &cli.IntFlag{Name: "desc-length", Value: 200, Usage: "Max description length"},
        },
        Action: runFiles,
    }
}
```

**Testing:**
- [ ] Unit tests for query functions
- [ ] Integration tests for CLI commands
- [ ] Test table formatting, JSON output
- [ ] Test pagination (limit, offset)

---

### Phase 3: Output Formatting (Week 2-3)

**Goal:** Pretty tables, JSON output, field selection, format options.

**Tasks:**

1. **Table rendering** (`internal/ui/`)
   - [ ] Extend existing `ui` package (already uses `go-pretty`)
   - [ ] `table.go` - Generic table builder
   - [ ] Format collections table
   - [ ] Format file list table with custom fields
   - [ ] Handle long descriptions (truncate with ellipsis)

2. **JSON output**
   - [ ] Add `--json` flag support to all commands
   - [ ] Structured JSON marshaling
   - [ ] Pretty-print JSON (indent)

3. **Field selection** (`--fields` flag)
   - [ ] Parse comma-separated field list
   - [ ] Validate fields against available options
   - [ ] Filter table columns based on selection

4. **Format selection** (`--format` flag)
   - [ ] Support `table`, `json`, `csv` formats
   - [ ] CSV output for `dox files`

**Testing:**
- [ ] Test table rendering with various field combinations
- [ ] Test JSON output structure
- [ ] Test CSV format
- [ ] Test truncation and formatting edge cases

---

### Phase 4: Configuration Support (Week 3)

**Goal:** Add `[display]` section to `dox.toml` and config loading.

**Tasks:**

1. **Extend config** (`internal/config/`)
   - [ ] Add `Display` struct to `types.go`
   - [ ] Add `[display]` section parsing in `config.go`
   - [ ] Set defaults for all display options
   - [ ] Validate display config

2. **Config precedence**
   - [ ] CLI flags override config
   - [ ] Config overrides built-in defaults
   - [ ] Document precedence in `dox init` template

3. **Update `dox init`**
   - [ ] Add `[display]` section to generated `dox.toml`
   - [ ] Include comments explaining each option
   - [ ] Show example values

**Config Struct:**
```go
type Display struct {
    DefaultLimit      int      `koanf:"default_limit"`
    DescriptionLength int      `koanf:"description_length"`
    LineNumbers       bool     `koanf:"line_numbers"`
    Format            string   `koanf:"format"`
    ListFields        []string `koanf:"list_fields"`
}
```

**Testing:**
- [ ] Test config loading with `[display]` section
- [ ] Test defaults when config not present
- [ ] Test CLI flag override
- [ ] Test validation (invalid format, invalid fields)

---

### Phase 5: Documentation & Examples (Week 3-4)

**Goal:** Update README, add usage examples, create demo video/gif, write guides.

**Tasks:**

1. **Update README.md**
   - [ ] Add "Query Documentation" section after "Usage"
   - [ ] Quick start example:
     ```bash
     dox sync
     dox collections
     dox files goreleaser
     dox cat goreleaser docs/install.md
     ```
   - [ ] Document all 4 commands with examples and options
   - [ ] Add "AI Agent Integration" section:
     - [ ] Example workflow for Claude/GPT
     - [ ] Example: `dox files goreleaser --json | jq` for programmatic use
     - [ ] Explain how this replaces MCP servers
   - [ ] Document configuration options (`[display]` section)
   - [ ] Add troubleshooting subsection (common errors)

2. **Create comprehensive usage guide** (`docs/query.md`)
   - [ ] **Discovery workflow:**
     - [ ] How to list collections
     - [ ] How to browse files in a collection
     - [ ] How to search for specific files (use glob patterns)
   - [ ] **Reading workflow:**
     - [ ] How to read entire files
     - [ ] How to read file sections (pagination)
     - [ ] How to view file structure (outline)
   - [ ] **File type handling:**
     - [ ] Markdown: heading extraction
     - [ ] MDX: how JSX is handled
     - [ ] Text: simple first-line description
     - [ ] TSX: documentation vs code component detection
   - [ ] **Output formats:**
     - [ ] Table format (default)
     - [ ] JSON format (for AI/scripts)
     - [ ] CSV format (for spreadsheets)
   - [ ] **Configuration guide:**
     - [ ] How to set defaults in `dox.toml`
     - [ ] Override precedence (CLI > config > defaults)

3. **Create file type guide** (`docs/file-types.md`)
   - [ ] Detailed explanation of each file type
   - [ ] Examples of description extraction
   - [ ] Examples of outline extraction
   - [ ] TSX component detection heuristic explained
   - [ ] Edge cases and limitations

4. **Create troubleshooting guide** (`docs/troubleshooting.md`)
   - [ ] **"Manifest not found"**
     - [ ] Cause: Haven't run `dox sync` yet
     - [ ] Solution: Run `dox sync`
   - [ ] **"Manifest corrupted"**
     - [ ] Cause: Interrupted sync, disk error
     - [ ] Solution: Delete `.dox/manifest.json`, run `dox sync`
   - [ ] **"Collection not found"**
     - [ ] Cause: Typo, collection not synced
     - [ ] Solution: Run `dox collections` to see available
   - [ ] **"File not found"**
     - [ ] Cause: File deleted after sync, path typo
     - [ ] Solution: Re-sync or check path
   - [ ] **Large manifest file (>100MB)**
     - [ ] Cause: Very large collection
     - [ ] Solution: Reduce patterns, use excludes
   - [ ] **Slow manifest generation**
     - [ ] Cause: Many large files
     - [ ] Solution: Use `--skip-large` flag (if implemented)

5. **Update CLI help text**
   - [ ] `dox collections --help`:
     ```
     List all documentation collections

     Usage:
       dox collections [options]

     Options:
       --json        Output as JSON
       --limit N     Show first N collections

     Examples:
       dox collections
       dox collections --json | jq '.[] | .name'
     ```
   - [ ] `dox files --help`:
     ```
     List files in a collection

     Usage:
       dox files <collection> [options]

     Options:
       --json             Output as JSON
       --limit N          Show first N files (default: 50)
       --all              Show all files
       --format FORMAT    Output format: table, json, csv
       --fields FIELDS    Comma-separated fields: path,type,lines,size,description,modified
       --desc-length N    Max description length (default: 200)

     Examples:
       dox files goreleaser
       dox files goreleaser --all --json
       dox files goreleaser --fields path,lines,description
     ```
   - [ ] `dox cat --help`:
     ```
     Read file contents

     Usage:
       dox cat <collection> <file> [options]

     Options:
       --json              Output as JSON with metadata
       --no-line-numbers   Don't show line numbers
       --offset N          Start at line N
       --limit N           Show N lines

     Examples:
       dox cat goreleaser docs/install.md
       dox cat goreleaser docs/install.md --offset 50 --limit 20
       dox cat goreleaser docs/install.md --json | jq .content
     ```
   - [ ] `dox outline --help`:
     ```
     Show file structure (headings, exports)

     Usage:
       dox outline <collection> <file> [options]

     Options:
       --json    Output as JSON

     Examples:
       dox outline goreleaser docs/install.md
       dox outline solidjs components/Button.tsx --json
     ```

6. **Create migration guide** (`docs/migration.md`)
   - [ ] **Upgrading from pre-query version:**
     - [ ] Existing sync behavior unchanged
     - [ ] Run `dox sync` to generate manifest
     - [ ] New commands available immediately
   - [ ] **Configuration migration:**
     - [ ] Add `[display]` section to `dox.toml` (optional)
     - [ ] Defaults work without config
   - [ ] **Breaking changes:** None (purely additive)

7. **Update `dox init` template**
   - [ ] Add `[display]` section with comments:
     ```toml
     # Query command defaults
     [display]
     # Number of files to show in 'dox files' (0 = all)
     default_limit = 50

     # Maximum length for file descriptions
     description_length = 200

     # Show line numbers in 'dox cat' by default
     line_numbers = true

     # Default output format: table, json, csv
     format = "table"

     # Default fields for 'dox files' table
     list_fields = ["path", "type", "lines", "size", "description"]
     ```

8. **Demo materials**
   - [ ] Create asciinema recording:
     - [ ] Show `dox sync` generating manifest
     - [ ] Show `dox collections`
     - [ ] Show `dox files goreleaser`
     - [ ] Show `dox cat` with pagination
     - [ ] Show `dox outline`
     - [ ] Show JSON output piped to `jq`
   - [ ] Add recording to README
   - [ ] Create GIF for GitHub README

9. **API documentation** (`docs/api.md`)
   - [ ] Document manifest JSON schema
   - [ ] Document query package exported functions (for library use)
   - [ ] Document parser interface
   - [ ] Example: reading manifest programmatically

10. **Update CONTRIBUTING.md**
    - [ ] Add section on query feature development
    - [ ] Document parser addition process (new file types)
    - [ ] Testing requirements for new parsers

**Testing:**
- [ ] Manually test all README examples
- [ ] Verify all help text examples work
- [ ] Test all `docs/` examples
- [ ] Proofread all documentation
- [ ] Test asciinema recording on fresh terminal

---

### Phase 6: Testing & Polish (Week 4)

**Goal:** Comprehensive testing, performance optimization, edge case handling.

**Tasks:**

1. **Comprehensive testing suite**
   - [ ] **End-to-end tests:**
     - [ ] Full workflow: `dox sync` → `dox collections` → `dox files` → `dox cat`
     - [ ] Test with real repos: goreleaser, solidjs, agent-browser
     - [ ] Verify JSON output structure for all commands
     - [ ] Test command chaining: `dox files goreleaser --json | jq`

   - [ ] **Error condition tests:**
     - [ ] Manifest not found → error with hint
     - [ ] Manifest corrupted (invalid JSON) → error with hint
     - [ ] Manifest version mismatch → warning + continue
     - [ ] Collection not found → list available collections
     - [ ] File not found in collection → error with file path
     - [ ] Invalid `--fields` value → error with valid options
     - [ ] Invalid `--format` value → error
     - [ ] Offset > file lines → empty output
     - [ ] Limit = 0 → no output

   - [ ] **Large collection tests:**
     - [ ] 1,000 files: manifest generation time
     - [ ] 10,000 files: manifest generation time + memory usage
     - [ ] `dox files` response time with 10k file manifest
     - [ ] Manifest file size with 10k files

   - [ ] **File type coverage matrix:**
     | File Type | Parse | List | Cat | Outline |
     |-----------|-------|------|-----|---------|
     | .md (ATX headings) | ✓ | ✓ | ✓ | ✓ |
     | .md (Setext) | ✓ | ✓ | ✓ | ✓ |
     | .md (code-only) | ✓ | ✓ | ✓ | ✓ |
     | .mdx (with JSX) | ✓ | ✓ | ✓ | ✓ |
     | .txt (plain) | ✓ | ✓ | ✓ | ✓ |
     | .tsx (documentation) | ✓ | ✓ | ✓ | ✓ |
     | .tsx (code) | ✓ | ✓ | ✓ | ✓ |
     | .ts (exports) | ✓ | ✓ | ✓ | ✓ |

   - [ ] **Edge case tests:**
     - [ ] Empty files (all types)
     - [ ] Binary files (skip with warning)
     - [ ] Files with BOM (strip correctly)
     - [ ] Invalid UTF-8 (skip with warning)
     - [ ] Very large files >50MB (skip parsing)
     - [ ] Very long paths >260 chars (truncate display)
     - [ ] Filenames with special chars (quotes, newlines)
     - [ ] Empty collection (zero matching files)
     - [ ] Collection deleted from config but in manifest
     - [ ] Symlinked files
     - [ ] Files deleted from disk after sync

2. **Performance optimization**
   - [ ] **Benchmark manifest generation:**
     - [ ] `BenchmarkManifestGenerate100Files` - Target: <1s
     - [ ] `BenchmarkManifestGenerate1000Files` - Target: <5s
     - [ ] `BenchmarkManifestGenerate10000Files` - Target: <30s
     - [ ] Memory profile: check for leaks, track peak usage

   - [ ] **Benchmark manifest loading:**
     - [ ] `BenchmarkManifestLoad100Files` - Target: <10ms
     - [ ] `BenchmarkManifestLoad1000Files` - Target: <100ms
     - [ ] `BenchmarkManifestLoad10000Files` - Target: <500ms
     - [ ] Memory profile: track allocation size

   - [ ] **Benchmark query operations:**
     - [ ] `BenchmarkQueryListCollection1000` - Target: <50ms
     - [ ] `BenchmarkQueryCatFile` - Target: <10ms (file read only)
     - [ ] `BenchmarkQueryOutline` - Target: <5ms (manifest lookup)
     - [ ] `BenchmarkQuerySearchFiles` - Target: <100ms for 1k files

   - [ ] **Optimize parser performance:**
     - [ ] Profile regex matching in TSX parser
     - [ ] Cache compiled regex patterns
     - [ ] Limit description extraction to first 1KB of file

   - [ ] **Memory optimization:**
     - [ ] Stream file reading (don't load entire file at once)
     - [ ] Limit manifest size (warn if >100MB)

3. **Error handling polish**
   - [ ] All errors use `oops` with error codes
   - [ ] All errors have actionable hints
   - [ ] Test error messages for clarity:
     - [ ] "Manifest not found. Run 'dox sync' to generate it."
     - [ ] "Collection 'foo' not found. Available: goreleaser, hono"
     - [ ] "Manifest corrupted. Delete .dox/manifest.json and run 'dox sync'."
   - [ ] Validate all user input early (collection names, file paths, flags)
   - [ ] Consistent exit codes (see error handling section)

4. **Platform testing**
   - [ ] Linux (Ubuntu 22.04, Debian)
   - [ ] macOS (Intel, Apple Silicon)
   - [ ] Windows (10, 11)
   - [ ] FreeBSD
   - [ ] Test path handling (Windows `\` vs Unix `/`)
   - [ ] Test file permissions
   - [ ] Test with different terminal encodings

5. **Concurrent access testing**
   - [ ] Multiple `dox files` processes simultaneously
   - [ ] `dox sync` while `dox cat` is running
   - [ ] Verify atomic manifest writes (no partial reads)

**Testing deliverables:**
- [ ] Test coverage >80% for all new packages
- [ ] All edge cases have explicit tests
- [ ] Performance benchmarks documented
- [ ] Platform compatibility matrix

---

## File Structure Changes

```
dox/
├── cmd/dox/
│   ├── main.go                 # Register new commands
│   ├── collections.go          # NEW: dox collections
│   ├── files.go                # NEW: dox files
│   ├── cat.go                  # NEW: dox cat
│   └── outline.go              # NEW: dox outline
├── internal/
│   ├── config/
│   │   ├── config.go           # MODIFY: Add Display config
│   │   └── types.go            # MODIFY: Add Display struct
│   ├── manifest/               # NEW PACKAGE
│   │   ├── types.go            # Manifest, Collection, FileInfo, Outline structs
│   │   ├── manifest.go         # Load/Save with atomic writes, versioning
│   │   ├── generator.go        # Generate manifest from synced files
│   │   ├── reader.go           # Query manifest (get collection, file, etc.)
│   │   └── validator.go        # Version validation, corruption detection
│   ├── parser/                 # NEW PACKAGE
│   │   ├── parser.go           # Parser interface and ParseResult type
│   │   ├── utils.go            # Binary detection, UTF-8 validation, BOM, file type
│   │   ├── markdown.go         # Markdown parser (uses gomarkdown)
│   │   ├── mdx.go              # MDX parser (frontmatter + markdown)
│   │   ├── text.go             # Text parser
│   │   └── typescript.go       # TypeScript/TSX parser (JSX headings + exports)
│   ├── query/                  # NEW PACKAGE
│   │   ├── query.go            # Query functions
│   │   ├── collections.go      # List collections
│   │   ├── files.go            # List files
│   │   ├── cat.go              # Read files
│   │   └── outline.go          # Show outline
│   ├── sync/
│   │   └── sync.go             # MODIFY: Generate manifest after sync
│   └── ui/
│       └── table.go            # MODIFY: Extend table rendering
└── .dox/
    ├── manifest.json           # NEW: Generated manifest
    └── [synced files...]
```

---

## Dependencies

### New Dependencies

```go
// Markdown parsing (for .md and .mdx files)
// Note: This is a pre-release version. Consider using a stable release if available.
github.com/gomarkdown/markdown v0.0.0-20231222211730-1d6d20845b47
```

**Installation:**
```bash
go get github.com/gomarkdown/markdown@v0.0.0-20231222211730-1d6d20845b47
```

**Usage in parsers:**
```go
import (
    "github.com/gomarkdown/markdown/ast"
    "github.com/gomarkdown/markdown/parser"
)

// Parse markdown and extract headings
md := []byte("# Title\n\nContent...")
doc := parser.NewWithExtensions(parser.CommonExtensions).Parse(md)

// Walk AST to extract headings using WalkFunc (function callback variant)
ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
    if heading, ok := node.(*ast.Heading); ok && entering {
        // heading.Level gives h1-h6 level
        // heading.Children contain the text nodes
    }
    return ast.GoToNext
})
```

### Existing Dependencies (Reused)

No changes to existing dependencies:

- `github.com/jedib0t/go-pretty/v6` - Table rendering (already in use)
- `github.com/knadh/koanf/v2` - Config parsing (already in use)
- `github.com/samber/oops` - Error handling (already in use)

### Standard Library (No Dependencies)

These features use only stdlib:

- **TSX/TS parsing:**
  - `regexp` - JSX heading pattern matching
  - `strings` - Text extraction and cleaning
  - `unicode/utf8` - UTF-8 validation

- **Text file parsing:**
  - `bufio` - Line-by-line reading
  - `strings` - Text processing

- **Binary file detection:**
  - `bytes` - Null byte detection
  - `unicode/utf8` - UTF-8 validation

- **JSON output:**
  - `encoding/json` - JSON marshaling

- **File operations:**
  - `os` - File reading, stat, rename, temp files
  - `io` - Reader/writer interfaces
  - `path/filepath` - Path manipulation

### Regex Patterns Used

For reference, these are the regex patterns used in TypeScript parser:

```go
const (
    // JSX heading detection: <h1>Text</h1>, <h2>Text</h2>, etc.
    jsxHeadingPattern = `<h([1-6])[^>]*>(.*?)</h\1>`

    // Export detection: export const Foo, export function Bar, etc.
    exportPattern = `^\s*export\s+(const|function|interface|type|class)\s+(\w+)`

    // JSDoc comment extraction: /** ... */
    jsdocPattern = `/\*\*\s*\n([^*]|\*(?!/))*\*/`
)
```

**Note:** These patterns are compiled once at initialization for performance:
```go
var (
    jsxHeadingRegex = regexp.MustCompile(jsxHeadingPattern)
    exportRegex     = regexp.MustCompile(exportPattern)
    jsdocRegex      = regexp.MustCompile(jsdocPattern)
)
```

---

## Migration & Backwards Compatibility

**Backwards Compatibility:**
- Existing `dox sync` behavior unchanged
- Manifest generation is additive (doesn't break existing workflows)
- New commands are opt-in (users can ignore if not needed)

**Migration Path:**
1. Update dox to new version
2. Run `dox sync` to generate manifest
3. Use new query commands (`collections`, `list`, `cat`, `outline`)

**Version Compatibility:**
- Manifest format versioned (`"version": "1.0.0"`)
- Future changes: add fields without breaking old clients
- If manifest version mismatch: regenerate with `dox sync --force`

---

## Timeline

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| Phase 1: Manifest Generation | Week 1 | `.dox/manifest.json` generated on sync |
| Phase 2: CLI Commands | Week 2 | All 4 commands functional |
| Phase 3: Output Formatting | Week 2-3 | Tables, JSON, field selection |
| Phase 4: Configuration | Week 3 | `[display]` in `dox.toml` |
| Phase 5: Documentation | Week 3-4 | README, examples, demos |
| Phase 6: Testing & Polish | Week 4 | Production-ready release |

**Total Estimated Time:** 4 weeks

---

## Future Enhancements (Out of Scope)

**Phase 7+ (Optional):**
- Full-text search using bleve (embedded search engine)
- Fuzzy file name search
- Tag/category support for collections
- Watch mode: auto-regenerate manifest on file changes
- Export manifest to other formats (SQLite, msgpack)
- Smart chunking hints for large files (for RAG systems)
- Syntax highlighting in `dox cat` output
- Support for more file types (`.rs`, `.go`, `.py` with structure parsing)

---

## Sync Integration Details

### Exact Hook Point

Manifest generation occurs in `internal/sync/sync.go` after `lock.Save()`:

```go
// In sync.Run() function, approximately line 155
if err := lock.Save(); err != nil {
    return oops.Wrapf(err, "failed to save lock file")
}

// NEW: Generate manifest
log.Info("Generating manifest...")
if err := manifest.Generate(ctx, s.cfg); err != nil {
    // Non-fatal: log warning but don't fail the sync
    log.Warnf("Failed to generate manifest: %v", err)
    // Users can still use synced docs, just no query commands
}
```

### Manifest Generation Flow

```
sync.Run()
  ├─ Download files from sources
  ├─ lock.Save() → .dox/.dox.lock
  └─ manifest.Generate()
       ├─ Walk .dox/ directory
       ├─ For each file:
       │    ├─ Detect file type (.md, .mdx, .txt, .tsx, .ts)
       │    ├─ Check if binary → skip with warning
       │    ├─ Check size (>50MB) → add to manifest, skip parsing
       │    ├─ Read file content
       │    ├─ Call appropriate parser
       │    ├─ Extract description + outline
       │    └─ Add FileInfo to manifest
       ├─ Group files by collection (source name)
       └─ Write .dox/manifest.json (atomic)
```

### Atomic Write Strategy

```go
// Prevent partial reads during manifest write
func (m *Manifest) Save(path string) error {
    tempPath := path + ".tmp"

    // Write to temp file
    if err := writeJSON(tempPath, m); err != nil {
        return err
    }

    // Atomic rename
    if err := os.Rename(tempPath, path); err != nil {
        return err
    }

    return nil
}
```

### Partial Sync Behavior

**Question:** Should manifest be regenerated during partial syncs (e.g., `dox sync goreleaser`)?

**Decision:** Yes, always regenerate full manifest
- **Reason:** Manifest should reflect complete state of `.dox/` directory
- **Alternative:** Could implement incremental updates, but adds complexity
- **Trade-off:** Full regeneration is simpler and more reliable

### Stale Collection Handling

**Scenario:** Collection removed from `dox.toml` but files still in `.dox/`

**Behavior:**
- `dox sync` regenerates manifest, includes all files in `.dox/`
- `dox collections` shows the collection with note: "(not in config)"
- User can manually delete the directory or run `dox clean`

---

## Performance Optimization Strategy

### V1 Approach: Simple and Measure

**No caching implemented beyond:**
1. **Regex pattern compilation** - Compiled once at package init, reused across all parser calls
2. **OS-level file system caching** - Operating system automatically caches recently read files

**Rationale:**
- CLI tools are stateless - each invocation runs and exits independently
- No shared state between command invocations (each is a new process)
- OS provides file caching for repeated reads (subsequent `dox files` hits page cache)
- JSON parsing is fast enough for expected manifest sizes (<500ms for 10k files)
- Premature optimization adds complexity without proven benefit

**Benchmarks to Validate Assumptions:**

Required benchmarks in Phase 6:
```go
BenchmarkManifestLoad100Files      // Target: <10ms
BenchmarkManifestLoad1000Files     // Target: <100ms
BenchmarkManifestLoad10000Files    // Target: <500ms

BenchmarkQueryListCollection1000   // Target: <50ms
BenchmarkQueryCatFile              // Target: <10ms
BenchmarkQueryOutline              // Target: <5ms
```

**Decision Criteria:**
- If manifest loading >500ms for 10k files → implement optimization
- If memory usage >500MB for 10k files → implement optimization
- If any query operation >100ms → investigate

### Future Optimizations (Post-V1, If Benchmarks Show Need)

**Option 1: SQLite Manifest** (Recommended for Large Collections)

```
Structure:
.dox/manifest.db

Schema:
  collections (id, name, type, source, last_sync, file_count, total_size)
  files (id, collection_id, path, type, size, lines, description, modified)
  headings (id, file_id, level, text, line)
  exports (id, file_id, type, name, line)

Benefits:
✓ Indexed queries (fast collection/file lookup by name)
✓ Partial loading (only load requested collection)
✓ Built-in query optimization and caching (SQLite page cache)
✓ Efficient filtering and sorting
✓ Handle 100k+ files easily

Tradeoffs:
✗ More complex implementation
✗ Requires SQLite library (modernc.org/sqlite for pure Go, or mattn/go-sqlite3 with CGO)
✗ Not human-readable (can't inspect with jq)
✗ Harder to debug

When to use: >10k files OR query performance >500ms
```

**Option 2: Manifest Sharding** (Good for Moderate Collections)

```
Structure:
.dox/manifests/
  _index.json          # Collection list with metadata
  goreleaser.json      # Just goreleaser files
  hono.json           # Just hono files
  solidjs.json        # Just solidjs files

Benefits:
✓ Load only requested collection
✓ Parallel generation during sync
✓ Smaller files, faster parsing
✓ Still JSON (human-readable)

Tradeoffs:
✗ More files to manage
✗ `dox collections` needs to scan all shards
✗ Atomic updates across multiple files

When to use: 5-10k files OR multiple large collections
```

**Option 3: Manifest Compression** (Good for I/O Bound Systems)

```
Structure:
.dox/manifest.json.gz

Benefits:
✓ 5-10x smaller file size
✓ Faster disk I/O (especially on slow disks)
✓ Still JSON structure (decompress to read)

Tradeoffs:
✗ CPU overhead for decompression
✗ Can't inspect directly (need to decompress first)
✗ Atomic writes more complex

When to use: Slow disk I/O OR very large manifests >50MB
```

**Option 4: Long-Running Server Mode** (Future Feature)

```bash
# Hypothetical future feature
dox serve --port 8080

# Or Unix socket
dox serve --socket /tmp/dox.sock
```

```
Benefits:
✓ Keep manifest in memory across requests
✓ Watch for file changes, auto-reload
✓ Enable more complex queries (full-text search)
✓ Could expose HTTP API or Unix socket

Implementation:
- Load manifest once on startup
- Keep in memory
- Watch .dox/ for changes (fsnotify)
- Reload manifest on change
- Serve queries via HTTP or socket

When to use: Interactive tools, IDE integrations, frequent queries
```

### Optimization Decision Tree

```
Start: Is manifest loading >500ms for your collection?
  ├─ No → No optimization needed, current design is fine
  └─ Yes → How many files?
      ├─ <5k files → Check if slow disk I/O
      │   ├─ Yes → Try compression (Option 3)
      │   └─ No → Profile and optimize JSON parsing
      ├─ 5-10k files → Use sharding (Option 2)
      └─ >10k files → Use SQLite (Option 1)

Alternative: Do you have frequent repeated queries?
  ├─ No → Current design is fine
  └─ Yes → Consider server mode (Option 4)
```

### Regex Compilation Caching (Already Implemented)

```go
// internal/parser/typescript.go
package parser

import "regexp"

var (
    // Compiled once at package initialization
    jsxHeadingRegex = regexp.MustCompile(`<h([1-6])[^>]*>(.*?)</h\1>`)
    exportRegex     = regexp.MustCompile(`^\s*export\s+(const|function|interface|type|class)\s+(\w+)`)
    jsdocRegex      = regexp.MustCompile(`/\*\*\s*\n([^*]|\*(?!/))*\*/`)
)

// Used in Parse() without recompilation
func (p *TypeScriptParser) Parse(path string, content []byte) (*ParseResult, error) {
    // Regex already compiled, just use it
    matches := jsxHeadingRegex.FindAllSubmatch(content, -1)
    // ...
}
```

**Benefits:**
- No compilation overhead per file
- Significant speedup during `dox sync` with many TSX files
- Already planned in implementation

### Lazy Loading (Potential Micro-Optimization)

```go
// Instead of loading all file metadata:
type Collection struct {
    Files []FileInfo // Loads all files into memory
}

// Load on demand:
type Collection struct {
    filesLoaded bool
    files       []FileInfo
}

func (c *Collection) GetFiles() ([]FileInfo, error) {
    if !c.filesLoaded {
        // Load from manifest on first access
    }
    return c.files, nil
}
```

**When to consider:**
- `dox collections` doesn't need file lists, only counts
- Could save memory if just listing collections
- But adds complexity for minimal gain
- **Decision:** Not worth it for V1

### Summary

**V1 Implementation:**
- ✅ No caching beyond regex compilation and OS file cache
- ✅ Performance benchmarks to validate assumptions
- ✅ Clear targets: <100ms for 1k files, <500ms for 10k files

**Post-V1 (If Needed):**
- Option 1: SQLite for >10k files
- Option 2: Sharding for 5-10k files
- Option 3: Compression for slow I/O
- Option 4: Server mode for frequent queries

**Monitor in the wild:**
- Gather telemetry on manifest sizes
- Track query performance in real usage
- Optimize based on actual user data, not assumptions

---

## Troubleshooting Reference

### Common Issues & Solutions

| Issue | Symptom | Cause | Solution |
|-------|---------|-------|----------|
| Manifest not found | `MANIFEST_NOT_FOUND` error | Haven't run sync since upgrade | Run `dox sync` |
| Manifest corrupted | `MANIFEST_CORRUPTED` error | Interrupted write, disk error | Delete `.dox/manifest.json`, run `dox sync` |
| Version mismatch | Warning on load | Old manifest, new dox version | Re-run `dox sync` to upgrade |
| Collection not found | `COLLECTION_NOT_FOUND` error | Typo or collection not synced | Run `dox collections` to list available |
| File not found | `FILE_NOT_FOUND` error | File deleted after sync | Re-run `dox sync` or check file path |
| Empty collection | "No files found" message | No files matched patterns | Check patterns in `dox.toml` |
| Large manifest | Slow query commands | 10,000+ files | Normal, optimize with better patterns |
| Binary files listed | N/A (shouldn't happen) | Exclude patterns insufficient | Add to global `excludes` in config |
| UTF-8 errors | Parse warnings during sync | Non-UTF-8 files in collection | Add to excludes or convert to UTF-8 |

---

## Resolved Decisions

All major design questions have been resolved:

1. **Binary file handling:** ✅ Skip binary files during parsing, add warning in sync output
   - Use null byte detection in first 512 bytes
   - Log warning: "Skipped binary file: {path}"

2. **Large file handling:** ✅ Files >50MB skip parsing, included in manifest with size info
   - Add to manifest: `{description: "(large file - parsing skipped)", warning: "file_too_large"}`
   - Files >100k lines: parse normally but log performance warning

3. **Manifest invalidation:** ✅ Regenerated on every `dox sync`
   - Always full regeneration (not incremental)
   - Simpler, more reliable than incremental updates
   - Performance acceptable (<30s for 10k files)

4. **TypeScript parsing:** ✅ Regex-based extraction (no AST parsing)
   - JSX heading extraction for documentation components
   - Export detection for code components
   - Heuristic: ≥2 JSX headings with content = documentation
   - Fallback to exports if <2 headings

5. **CSV format:** ✅ Include CSV support
   - Minimal effort with go-pretty
   - Useful for data analysis in spreadsheets
   - Example: `dox files goreleaser --format csv > files.csv`

6. **Markdown library:** ✅ Use `github.com/gomarkdown/markdown`
   - Mature, well-maintained
   - Supports ATX and Setext headings
   - AST-based parsing for reliability

7. **Error handling:** ✅ Use `oops` library consistently
   - All errors have codes (e.g., `MANIFEST_NOT_FOUND`)
   - All errors have hints (e.g., "Run 'dox sync' first")
   - Non-fatal parser errors log warnings, continue

8. **Concurrent access:** ✅ Read-only manifest access is safe
   - Multiple queries can run in parallel
   - Atomic write (temp file + rename) prevents partial reads
   - No locking needed for queries

9. **Stale collections:** ✅ Show in `dox collections` with note
   - Display: `collection_name (not in config)`
   - User can manually clean up or ignore

10. **Manifest location:** ✅ Always `.dox/manifest.json` (not configurable)
    - Relative to `output` directory from config
    - Consistent, predictable location

---

## Implementation Checklist

Use this checklist to track implementation progress and ensure nothing is missed.

### Phase 1: Manifest Generation
- [ ] Type definitions (`internal/manifest/types.go`)
  - [ ] Manifest, Collection, FileInfo structs
  - [ ] Outline, Heading, Export structs
  - [ ] OutlineType, ComponentType enums
- [ ] Manifest operations (`internal/manifest/manifest.go`)
  - [ ] Load/Save with atomic writes
  - [ ] Version validation
  - [ ] Corruption detection
- [ ] Parser interface (`internal/parser/parser.go`)
  - [ ] Parser interface definition
  - [ ] ParseResult struct
- [ ] Parser utilities (`internal/parser/utils.go`)
  - [ ] isBinary() - null byte detection
  - [ ] isValidUTF8() - UTF-8 validation
  - [ ] stripBOM() - BOM removal
  - [ ] DetectFileType() - extension mapping
- [ ] Markdown parser (`internal/parser/markdown.go`)
  - [ ] ATX heading extraction
  - [ ] Setext heading extraction
  - [ ] Description from first heading/paragraph
  - [ ] Handle code-only markdown
- [ ] MDX parser (`internal/parser/mdx.go`)
  - [ ] Reuse markdown parser
  - [ ] Ignore JSX imports
- [ ] Text parser (`internal/parser/text.go`)
  - [ ] Extract first non-empty line
  - [ ] Return OutlineTypeNone
- [ ] TypeScript parser (`internal/parser/typescript.go`)
  - [ ] JSX heading extraction (documentation)
  - [ ] Export detection (code)
  - [ ] JSDoc extraction
  - [ ] Component type heuristic (≥2 headings)
- [ ] Manifest generator (`internal/manifest/generator.go`)
  - [ ] Walk .dox/ directory
  - [ ] Call parsers for each file
  - [ ] Handle large files (>50MB)
  - [ ] Handle binary files
  - [ ] Group by collection
- [ ] Sync integration (`internal/sync/sync.go`)
  - [ ] Call manifest.Generate() after lock.Save()
  - [ ] Non-fatal error handling
  - [ ] Logging
- [ ] Unit tests (all parsers)
- [ ] Integration tests (sync → manifest)
- [ ] Edge case tests (binary, large, empty files)

### Phase 2: CLI Commands
- [ ] Query package (`internal/query/`)
  - [ ] Manifest loader with error handling
  - [ ] Collections query function
  - [ ] File list query function
  - [ ] File cat query function
  - [ ] File outline query function
- [ ] Collections command (`cmd/dox/collections.go`)
  - [ ] Table output
  - [ ] JSON output
  - [ ] Limit support
  - [ ] Error handling
- [ ] Files command (`cmd/dox/files.go`)
  - [ ] Table output with configurable fields
  - [ ] JSON output
  - [ ] CSV output
  - [ ] Pagination (limit, all)
  - [ ] Field selection
  - [ ] Description length truncation
- [ ] Cat command (`cmd/dox/cat.go`)
  - [ ] File reading with line numbers
  - [ ] Pagination (offset, limit)
  - [ ] JSON output
  - [ ] No-line-numbers flag
- [ ] Outline command (`cmd/dox/outline.go`)
  - [ ] Heading display (markdown/tsx-docs)
  - [ ] Export display (tsx-code/ts)
  - [ ] JSON output
- [ ] Command registration (`cmd/dox/main.go`)
- [ ] Help text for all commands
- [ ] Unit tests (all query functions)
- [ ] Integration tests (all commands)

### Phase 3: Output Formatting
- [ ] Table rendering (`internal/ui/table.go`)
  - [ ] Collections table
  - [ ] File list table with custom fields
  - [ ] Description truncation with ellipsis
  - [ ] Path truncation for long paths
- [ ] JSON output (all commands)
- [ ] CSV output (dox files)
- [ ] Field selection validation
- [ ] Format validation
- [ ] Tests for all formats

### Phase 4: Configuration
- [ ] Display config struct (`internal/config/types.go`)
- [ ] Config loading (`internal/config/config.go`)
- [ ] Config validation
- [ ] Precedence: CLI > config > defaults
- [ ] Update dox init template
- [ ] Tests for config loading

### Phase 5: Documentation
- [ ] README.md updates
  - [ ] Query Documentation section
  - [ ] AI Agent Integration examples
  - [ ] Configuration documentation
  - [ ] Troubleshooting subsection
- [ ] docs/query.md (usage guide)
- [ ] docs/file-types.md (file type handling)
- [ ] docs/troubleshooting.md
- [ ] docs/migration.md
- [ ] docs/api.md (manifest schema)
- [ ] CLI help text (all commands)
- [ ] Asciinema demo recording
- [ ] README GIF
- [ ] CONTRIBUTING.md updates

### Phase 6: Testing & Polish
- [ ] End-to-end tests (full workflow)
- [ ] Error condition tests (all error codes)
- [ ] Large collection tests (1k, 10k files)
- [ ] File type coverage matrix
- [ ] Edge case tests (all scenarios)
- [ ] Performance benchmarks
  - [ ] Manifest generation (100, 1k, 10k files)
  - [ ] Query operations (list, cat, outline)
- [ ] Memory profiling
- [ ] Platform testing (Linux, macOS, Windows, FreeBSD)
- [ ] Concurrent access tests
- [ ] Error message clarity review
- [ ] Test coverage >80%

### Final Checklist
- [ ] All error codes defined and used
- [ ] All errors have hints
- [ ] All commands have help text with examples
- [ ] All edge cases have explicit handling
- [ ] All file types tested
- [ ] Performance targets met
- [ ] Documentation complete and accurate
- [ ] Migration guide written
- [ ] CHANGELOG.md updated
- [ ] Version bump decided
- [ ] GoReleaser config updated (if needed)

---

## Success Criteria

Before merging, verify:

✅ **Functionality:**
- [ ] Can list all collections
- [ ] Can list files in any collection with accurate metadata
- [ ] Can read any file with pagination
- [ ] Can view outlines for all 4 supported file types
- [ ] JSON output works for all commands
- [ ] Configuration in `dox.toml` works correctly

✅ **Performance:**
- [ ] Manifest generation <30s for 10k files
- [ ] `dox files` responds in <100ms for 1000 file collection
- [ ] `dox cat` responds instantly (file read only)
- [ ] Memory usage acceptable (<500MB for 10k file manifest)

✅ **Usability:**
- [ ] AI agents can discover and read docs without external tools
- [ ] Commands follow familiar patterns (`gh`, `kubectl`)
- [ ] Help text is clear and includes examples
- [ ] Error messages are actionable
- [ ] Works on all supported platforms

✅ **Code Quality:**
- [ ] Test coverage >80%
- [ ] All edge cases handled
- [ ] Follows existing dox code patterns
- [ ] Uses `oops` for error handling
- [ ] No regression in existing functionality
- [ ] Documentation is complete

✅ **User Impact:**
- [ ] Existing users: No breaking changes, must run `dox sync` once
- [ ] New users: Feature available immediately after `dox sync`
- [ ] AI agents: Can replace MCP servers for documentation access

---

## Post-Implementation

After merging:

1. **Release Notes:**
   - [ ] Write release notes highlighting query commands
   - [ ] Include migration instructions
   - [ ] Include examples

2. **Announcement:**
   - [ ] Blog post or announcement about new feature
   - [ ] Show AI agent use cases
   - [ ] Include demo recording

3. **Community:**
   - [ ] Update GitHub README with badges (if applicable)
   - [ ] Announce in relevant communities (Reddit, HN, etc.)
   - [ ] Gather feedback

4. **Monitoring:**
   - [ ] Watch for bug reports
   - [ ] Monitor performance issues
   - [ ] Track feature usage (if telemetry available)

---

## Notes

- Keep implementation simple and focused
- Prioritize AI agent UX (JSON output, clear structure)
- Follow existing code patterns in dox codebase
- Comprehensive testing is critical for reliability
- Documentation is as important as code
- Error messages must be actionable and helpful
- Performance should be acceptable out of the box
- Edge cases must have explicit handling, not assumptions
