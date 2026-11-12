# Development tools

Development and isolated tool modules use Go 1.27.4. The CLI retains its Go 1.26
minimum; the root toolchain directive selects the patched development release.

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

Buildx v0.39.0 is pinned separately in `tools/buildx/`, keeping its Docker/Kubernetes
graph independent of the other tools. `scripts/with-buildx.sh <command> [args...]`
builds that tool through `go tool` and supplies an ephemeral `docker buildx`
dispatcher to the command. Other Docker calls go to the existing CLI. Builder
state is temporary (`BUILDX_CONFIG`); Docker configuration, credentials and daemon
selection are inherited unchanged. Nothing is installed globally. For example:

```sh
scripts/with-buildx.sh docker buildx version
scripts/with-buildx.sh go tool -modfile=tools/go.mod goreleaser release --snapshot
```

Use an owned temporary output directory for snapshots (set `dist` in a temporary
copy of `.goreleaser.yaml`); only use `--clean` for that directory. Do not publish
snapshots or push their image tags. Buildx's upstream `version` string is unset
when built with `go tool`; verify the actual pin with
`go version -m "$(go tool -modfile=tools/buildx/go.mod -n buildx)"`.

Run tidy from each module's directory: the repository root, `tools/`,
`tools/beads/`, `tools/actionlint/`, and `tools/buildx/`. Using `-modfile` from the root scans
the root packages instead of the tools module.
