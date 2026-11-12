# Audit regression cases

These scripts and `internal/source/audit_test.go` encode the audited v0.3.0
failures. Each case exercises its assertions, then passes the observed error to
`internal/testutil/regression`. The `knownFailing` registry accepts only a listed
failure with the expected evidence. An unexpected success fails with an
instruction to remove the entry; unlisted failures fail normally.

When fixing an audit finding, remove its entries from the registry in the same
change. Keep every regression and its assertions. Do not add entries to hide new
failures. The registry must be empty before preparing v0.4.0.

To reproduce the current failures without inversion:

```sh
DOX_AUDIT_ENFORCE=1 go test ./cmd/dox ./internal/source -run '^TestAudit' -count=1 -v
```

That command deliberately exits nonzero while audited bugs remain. Normal
`go test` runs enforce the registry. All HTTP fixtures are local; subprocess
commands run inside testscript temporary directories. Deletion probes keep
sentinels inside those directories. The absolute `/` output case checks config
validation only: it never invokes deletion against the filesystem root.

| Cases | Fixing issue |
| --- | --- |
| R1 output dot, parent, root; out and filename traversal | dox-deb.11 |
| R2 request timeout | dox-deb.14 |
| R3 403/429 token hints and successful final quota request | dox-deb.15 |
| P2 configured concurrency | dox-deb.16 |
| C3 add inference and aliases | dox-deb.18 |
| C4 non-GitHub host | dox-deb.17 |
| C5 hint and distinct exit code | dox-deb.13 |
| C6 search/cat line round trip | dox-deb.19 |
| P9 dry-run body reads | dox-deb.27 |

The container R10 repro belongs to dox-deb.20.
