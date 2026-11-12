# CLI scripts

Run with `go test ./cmd/dox -run TestScripts`. `testscript.Main` registers the
real `main` as `dox`; no separate CLI build is needed. The initial scripts cover
`--version` (the existing interface), `init` including duplicate-file failure,
list JSON, and URL sync/readback against a local HTTP server. `cli-format`,
`cli-config-root`, `cli-did-you-mean`, `doctor` and `completion` cover the shared
output formats and limits, the root `--config`/`DOX_CONFIG`, collection and file
suggestions, `dox doctor` and shell completion.

Every script gets its own temporary working directory. Keep destructive command
cases inside that directory. Tests use loopback HTTP and no external service.

Custom commands:

- `httpserve file`: serve one fixture file and export its base URL as `$SERVER`;
  the server closes when the script ends.
- `httpfail status`: serve a loopback HTTP endpoint that responds with `status`
  and export its base URL as `$FAIL_SERVER`.
- `envsubst input output`: expand script environment variables into a fixture,
  useful for generated `dox.toml` URLs.
- `pathenv KEY parts...`: join path components with the native separator and
  store the result in the script environment.
- `jsoncmp actual expected`: compare canonical JSON; `stdout` and `stderr` name
  the last command's output. Object-key order and whitespace are ignored; array
  order and exact numeric values are preserved. Environment placeholders inside
  expected JSON strings are escaped, including Windows backslashes.
- `doxexit code [args...]`: run `dox` with `args` and assert its exit status
  equals `code` while preserving `stdout` and `stderr` for subsequent checks.
- `doxinterrupt code args...`: start `dox` against a blocking loopback URL
  source, send `SIGINT` once connected, and assert it exits with `code`.

Use ordinary testscript commands to check stdout/stderr, command failure, and
files. Add each audit regression as a case before implementing its fix; the next
issue supplies the known-failure registry for existing bugs.
