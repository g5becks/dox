# Development tools

The root module contains CLI dependencies. `tools/go.mod` pins Task,
golangci-lint, Lefthook, GoReleaser, goimports, govulncheck, and benchstat. Run them from the
repository root, for example:

```sh
go tool -modfile=tools/go.mod task lint
go tool -modfile=tools/go.mod goimports -l cmd internal
```

Beads v0.49.6 has its own module in `tools/beads/`. Always invoke it through
`scripts/bd`, which disables CGO and the daemon. Its older Charm dependencies
cannot share the newer development tools' dependency graph.

Actionlint v1.7.12 is isolated in `tools/actionlint/`: its YAML v4 rc.3 API
is incompatible with the shared tools graph's rc.6. Run it with
`go tool -modfile=tools/actionlint/go.mod actionlint`.

Run tidy from each module's directory: the repository root, `tools/`,
`tools/beads/`, and `tools/actionlint/`. Using `-modfile` from the root scans
the root packages instead of the tools module.
