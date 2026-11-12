# Internal errors

`Error` carries one of seven categories and optional guidance and context.
`Error()` returns its unchanged message; `Unwrap()` exposes its cause. Wrap
context with `fmt.Errorf("context: %w", err)`, then use `errs.Wrap` when adding
metadata. `errs.New` handles leaf errors. `errors.Is` and
`errors.AsType[*errs.Error]` work through either wrapper.

Unknown configured sources include `ErrSourceNotFound`; failed HTTP responses
include `*HTTPStatusError`; exhausted quota errors include `*RateLimitError`
with the parsed reset time (zero when unknown). Classification does not change
request retry or quota decisions. CLI rendering is a separate issue.

The previous code strings remain in `Fields[LegacyCodeField]`. Existing fields
and hints are retained; a partial sync uses `PartialFailure`, while individual
failed downloads use `Network`. The migration inventory is recorded in
`dox-deb.12` notes.

| Previous code | New category (site count) |
| --- | --- |
| COLLECTION_NOT_FOUND | NotFound (5) |
| CONFIG_INVALID | Config (15) |
| CONFIG_NOT_FOUND | NotFound (3) |
| CONFIG_WRITE_ERROR | Config (5) |
| CSV_ERROR | Internal (6) |
| DOWNLOAD_FAILED | Network (6), PartialFailure (1) |
| FILE_NOT_FOUND | NotFound (2) |
| FILE_READ_ERROR | Internal (1) |
| GITHUB_API_ERROR | Network (10) |
| GITHUB_RATE_LIMIT | RateLimited (1) |
| INVALID_ARGS | Usage (8) |
| JSON_ERROR | Internal (6) |
| LOCK_ERROR | Internal (9) |
| MANIFEST_CORRUPTED | Internal (1) |
| MANIFEST_GENERATION_ERROR | Internal (1) |
| MANIFEST_NOT_FOUND | NotFound (1) |
| MANIFEST_READ_ERROR | Internal (1) |
| MANIFEST_WRITE_ERROR | Internal (7) |
| SEARCH_ERROR | Internal (1) |
| SOURCE_EXISTS | Usage (1) |
| SOURCE_NOT_FOUND | NotFound (3) |
| UNKNOWN_SOURCE_TYPE | Config (2) |
| WRITE_FAILED | Internal (13) |

Removing oops removes its OpenTelemetry, ULID and lo dependencies. Koanf
independently uses copystructure, which therefore remains linked until the
config-loader dependency is addressed. Do not claim it was removed.
