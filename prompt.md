 You are working in `/Users/takinprofit/Dev/dox`.

  Start with beads epic `dox-8i7` and implement the first task `dox-8i7.1` only.

  Before coding, read context in this order:
  1) `AGENTS.md`
  2) `SEARCH_PLAN.md`
  3) `bd show dox-8i7`
  4) `bd show dox-8i7.1`
  5) `internal/manifest/manifest.go`
  6) `cmd/dox/files.go`
  7) `cmd/dox/main.go`
  8) `internal/parser/parser.go`

  Run these commands to get started:
  ```bash
  cd /Users/takinprofit/Dev/dox
  git status --short
  bd prime --no-daemon
  bd ready --no-daemon --no-auto-flush
  bd show dox-8i7 --no-daemon --no-auto-flush
  bd show dox-8i7.1 --no-daemon --no-auto-flush
  bd update dox-8i7.1 --status in_progress --no-daemon --no-auto-flush

  Implement dox-8i7.1 exactly:

  - Create internal/search/search.go with:
      - MetadataResult
      - MetadataOptions
      - Metadata(m *manifest.Manifest, opts MetadataOptions) ([]MetadataResult, error) (compile-safe placeholder)
  - Create internal/search/content.go with:
      - ContentResult
      - ContentOptions
      - Content(m *manifest.Manifest, opts ContentOptions) ([]ContentResult, error) (compile-safe placeholder)
  - Add dependency:

  go get github.com/sahilm/fuzzy@latest

  - Keep JSON tags and error conventions consistent with existing codebase.

  Required validation before commit:

  task lint
  task build || go build ./...
  task test

  Then finish task with:

  git add .
  git commit -m "feat: scaffold search package contracts"
  bd close dox-8i7.1 --no-daemon --no-auto-flush
  bd sync --no-daemon

  After closing, report what changed and what next ready task is (bd ready).
