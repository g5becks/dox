# Changelog

## [Unreleased]

### Changed

- Building from source requires Go 1.26 or newer; development uses Go 1.27.3. Upgrade older Go installations before building. Go 1.26 retains compatibility with the oldest supported Go line while enabling the standard library modernizations.

### Maintenance

- Applied Go modernizers and simplified sorted map iteration and test contexts without changing CLI behavior.
- Git hooks now format staged Go files and check new lint findings before commits; full lint and race tests run before pushes. Install with `go tool -modfile=tools/go.mod lefthook install`.
- Moved development tools into `tools/go.mod` and pinned golangci-lint. Run Task with `go tool -modfile=tools/go.mod task` to use the repository's pinned version. Beads v0.49.6 is isolated in `tools/beads/go.mod`; continue using `scripts/bd`.
