# Contributing to dox

Thank you for your interest in contributing to dox! This guide will help you get started.

## Code of Conduct

Be respectful and constructive. We're here to build great software together.

## Getting Started

### Prerequisites

- **Go 1.25.7+** (check with `go version`)
- **Task** (optional but recommended): `go install github.com/go-task/task/v3/cmd/task@latest`
- **golangci-lint** (for linting): https://golangci-lint.run/usage/install/

### Clone and Build

```bash
git clone https://github.com/g5becks/dox.git
cd dox
go mod download
task build  # or: go build -o bin/dox ./cmd/dox
```

### Run Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/sync/...

# Using task
task test
```

### Lint

```bash
# Run all linters
golangci-lint run ./...

# Auto-fix issues where possible
golangci-lint run --fix ./...

# Using task
task lint
```

### Manual Testing

```bash
# Build and test locally
task build
./bin/dox init
./bin/dox sync --help
```

## Development Workflow

### 1. Create an Issue

Before starting work, [create an issue](https://github.com/g5becks/dox/issues/new) describing:
- The problem or feature
- Proposed solution (for features)
- Any breaking changes

### 2. Fork and Branch

```bash
# Fork the repo on GitHub, then:
git clone git@github.com:YOUR_USERNAME/dox.git
cd dox
git remote add upstream git@github.com:g5becks/dox.git

# Create a feature branch
git checkout -b feat/my-feature
# or
git checkout -b fix/issue-123
```

### 3. Make Changes

- Write clear, focused commits
- Follow existing code style
- Add tests for new functionality
- Update documentation as needed

### 4. Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add support for GitLab repos
fix: handle rate limit errors gracefully
docs: update README with new examples
test: add coverage for URL source
chore: update dependencies
refactor: simplify config validation
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Test changes
- `refactor`: Code restructuring
- `chore`: Tooling, dependencies, etc.
- `perf`: Performance improvements

### 5. Test Your Changes

```bash
# Run full test suite
go test ./...

# Run linter
golangci-lint run ./...

# Test the binary manually
task build
./bin/dox sync --dry-run
```

### 6. Push and Create PR

```bash
git push origin feat/my-feature
```

Then [create a Pull Request](https://github.com/g5becks/dox/compare) on GitHub.

**PR Guidelines:**
- Title should follow conventional commits format
- Describe what changed and why
- Link to related issues with `Fixes #123` or `Closes #123`
- Include screenshots for UI changes
- Mark as draft if work-in-progress

## Project Structure

```
dox/
├── cmd/dox/          # CLI entry point
├── internal/
│   ├── config/       # Configuration parsing & validation
│   ├── lockfile/     # Lock file management
│   ├── manifest/     # Manifest generation & persistence
│   ├── parser/       # File parsers (markdown, MDX, TypeScript, text)
│   ├── source/       # Source implementations (GitHub, URL)
│   ├── sync/         # Sync orchestration
│   └── ui/           # Terminal UI (tables, colors)
├── .goreleaser.yaml  # Release configuration
├── Taskfile.yml      # Task automation
└── README.md
```

## Code Style

### General

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Run `goimports` (or use your editor's format-on-save)
- Keep functions focused and under 100 lines
- Prefer clarity over cleverness

### Error Handling

We use [samber/oops](https://github.com/samber/oops) for rich errors:

```go
return nil, oops.
    Code("CONFIG_INVALID").
    With("path", configPath).
    Hint("Check that the file exists and is valid TOML").
    Wrapf(err, "loading config")
```

### Testing

- Test files live alongside source: `foo.go` → `foo_test.go`
- Use table-driven tests for multiple cases
- Test both success and error paths
- Use `t.Parallel()` for independent tests

Example:
```go
func TestMyFunction(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name string
        input string
        want string
        wantErr bool
    }{
        {name: "valid input", input: "foo", want: "FOO"},
        {name: "empty input", input: "", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            got, err := MyFunction(tt.input)
            if tt.wantErr {
                if err == nil {
                    t.Fatal("expected error, got nil")
                }
                return
            }

            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

## Adding a New Source Type

To add a new source (e.g., GitLab, BitBucket):

1. **Add config struct** in `internal/config/config.go`
2. **Implement `Source` interface** in `internal/source/newtype.go`
3. **Register in factory** (`internal/source/source.go`)
4. **Add tests** in `internal/source/newtype_test.go`
5. **Update docs** (README.md, config examples)

## Adding a New Parser

To add support for a new file type in the query feature:

1. **Implement `Parser` interface** in `internal/parser/newtype.go`:
   ```go
   type NewParser struct{}
   
   func (p *NewParser) CanParse(path string) bool {
       return strings.HasSuffix(path, ".ext")
   }
   
   func (p *NewParser) Parse(_ string, content []byte) (*ParseResult, error) {
       // Extract headings, description, etc.
       return &ParseResult{...}, nil
   }
   ```

2. **Register parser** in `internal/manifest/generator.go` parsers list
3. **Add tests** in `internal/parser/newtype_test.go`
4. **Update docs** (README.md supported file types)

Supported parsers: Markdown (.md), MDX (.mdx), TypeScript/TSX (.ts/.tsx), Text (.txt)

## Release Process

Releases are automated via GitHub Actions + goreleaser:

1. **Ensure main branch is stable**
   - All tests passing
   - No known critical bugs

2. **Tag a release**
   ```bash
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

3. **GitHub Actions will:**
   - Build binaries for all platforms
   - Create GitHub release with changelog
   - Publish to Homebrew tap
   - Upload packages (deb, rpm, etc.)

4. **Verify release**
   - Check [releases page](https://github.com/g5becks/dox/releases)
   - Test Homebrew install: `brew install g5becks/tap/dox`

## Getting Help

- **Questions?** [Open a discussion](https://github.com/g5becks/dox/discussions)
- **Bug?** [File an issue](https://github.com/g5becks/dox/issues/new)
- **PR feedback?** Comment on your PR or ping maintainers

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
