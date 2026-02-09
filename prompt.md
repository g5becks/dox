 ---
  You are working on the dox project — a Go CLI tool that syncs documentation from multiple sources into a local directory. A query feature was recently
  implemented (collections, files, cat, outline commands). A code audit found 21 issues across all severity levels. An epic (dox-bsv) with 14 tasks has
  been created to fix them all.

  Required Reading

  Before starting any work, read these files to understand the codebase and the fixes needed:

  Audit context

  QUERY_FEATURE_IMPLEMENTATION.md     # Original implementation plan (skim for design intent)

  Files you will be modifying (read ALL before starting)

  cmd/dox/main.go                     # CLI command registration, init template
  cmd/dox/cat.go                      # Cat command (3 bugs to fix)
  cmd/dox/collections.go              # Collections command (formatTime lives here)
  cmd/dox/files.go                    # Files command (uses intToString, formatSize)
  cmd/dox/outline.go                  # Outline command (formatSize lives here)
  internal/manifest/manifest.go       # Manifest types (ManifestPath stutter, Collection struct)
  internal/manifest/generator.go      # Generator (nilerr, LastSync, resolveSourceLocation, Dir)
  internal/parser/doc.go              # Redundant blank import (delete this file)
  internal/parser/mdx.go              # MDX parser (multi-line import bug)
  internal/parser/typescript.go       # TSX parser (duplicate heading line numbers)
  internal/parser/text.go             # Text parser (modernize: SplitSeq)
  internal/parser/utils.go            # Utils (modernize: SplitSeq in StripFrontmatter)
  README.md                           # Documentation (flags/config don't match actual CLI)

  Test files you will be modifying

  internal/manifest/generator_test.go
  internal/manifest/manifest_test.go
  internal/manifest/integration_test.go   # govet shadow, Generate signature
  internal/manifest/bench_test.go         # Empty Sources config, intrange
  internal/parser/integration_test.go     # Direct struct init bypassing constructors
  internal/parser/typescript_test.go      # Add duplicate heading tests
  internal/parser/mdx_test.go            # Add multi-line import tests
  testdata/doc-component.tsx              # Not realistic TSX

  Reference files (read for patterns, do NOT modify unless necessary)

  internal/config/types.go            # Display struct, Config struct, ApplyDefaults
  internal/config/types_test.go       # Display config tests
  internal/sync/sync.go               # Calls manifest.Generate (update caller)
  internal/lockfile/lockfile.go       # LockFile type, GetEntry, SyncedAt field
  internal/ui/sync.go                 # EventManifestError handler

  Quality Commands

  Every task must pass all three before committing:

  # Lint — must have ZERO new warnings (only pre-existing gocognit is acceptable)
  golangci-lint run ./...

  # Build — must compile cleanly
  go build ./...

  # Test — all tests must pass, including with race detector
  go test ./...
  go test -race ./...

  Work Tracking Commands

  bd ready                              # Show tasks with no blockers (start here)
  bd show <id>                          # Read full task description before starting
  bd update <id> --status=in_progress   # Claim a task before working on it
  bd close <id>                         # Mark complete after commit
  bd close <id1> <id2> ...              # Close multiple at once
  bd blocked                            # See what's unblocked after closing tasks
  bd sync                               # Run at session end to push beads state

  Dependency Graph

  Layer 0 (ready now, can be done in parallel):
    dox-lfs  Replace intToString with strconv.Itoa + remove doc.go
    dox-ub5  Fix all linter formatting/modernization issues
    dox-wym  Rename manifest.ManifestPath → manifest.Path
    dox-u78  Move formatSize/formatTime to shared helpers file

  Layer 1 (unblocks after Layer 0):
    dox-8zf  [P0 CRITICAL] Fix generator nilerr          ← blocked by ub5
    dox-ji0  Add Dir field + lockfile timestamps          ← blocked by ub5, wym
    dox-1gd  Fix resolveSourceLocation empty string       ← blocked by ub5
    dox-mrb  Fix MDX multi-line import stripping          ← blocked by ub5
    dox-s9e  Fix TSX duplicate heading line numbers       ← blocked by ub5
    dox-f47  Fix benchmark tests empty Sources            ← blocked by ub5
    dox-p7g  Fix testdata doc-component.tsx               ← blocked by ub5

  Layer 2 (unblocks after Layer 1):
    dox-ncq  Fix cat: config, off-by-one, path resolution ← blocked by lfs, ji0
    dox-tyu  Fix integration test constructor patterns     ← blocked by 8zf, ji0

  Layer 3 (final):
    dox-w7u  Fix README to match actual CLI               ← blocked by ncq

  Getting Started

  bd ready                           # See the 4 Layer 0 tasks
  bd show dox-lfs                    # Start with this one (quick win, unblocks Layer 2)
  bd update dox-lfs --status=in_progress

  Work through Layer 0 tasks first. After each task: lint, build, test, commit, bd close. Then run bd ready to see what's unblocked. After Layer 0,
  prioritize dox-8zf (P0 critical) and dox-ji0 (P1, unblocks the most downstream tasks). Finish with dox-w7u (README) last since it documents the final
  state.

  IMPORTANT: Read each task's full description with bd show <id> before starting — they contain exact code changes, file locations, and acceptance
  criteria. Do NOT skip reading the task description. Commit after each completed task with the message specified in the task.
