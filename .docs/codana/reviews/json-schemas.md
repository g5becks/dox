# JSON Schema Review

Analysis of current JSON output schemas in Codanna CLI.

## Current State

Two incompatible schemas exist:

### 1. `JsonResponse` (mcp commands)

Used by: `codanna mcp <tool> --json`

Source: `src/io/format.rs`

```json
{
  "status": "success",
  "code": "OK",
  "message": "Operation completed successfully",
  "system_message": "Found 3 matches. Consider using 'find_symbol'...",
  "data": [...],
  "error": null,
  "exit_code": 0,
  "meta": {
    "version": "0.1.0",
    "timestamp": "2025-01-13T10:00:00Z",
    "execution_time_ms": 42
  }
}
```

**Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `status` | `"success"` \| `"error"` | Operation outcome |
| `code` | string | Result code: `OK`, `NOT_FOUND`, `PARSE_ERROR`, etc. |
| `message` | string | Human-readable description |
| `system_message` | string? | AI assistant guidance (optional) |
| `data` | any? | Payload on success |
| `error` | object? | Error details with `suggestions[]` and `context` |
| `exit_code` | integer | Unix exit code (0-255) |
| `meta` | object? | Version, timestamp, execution time |

### 2. `UnifiedOutput` (retrieve commands)

Used by: `codanna retrieve <subcommand> --json`

Source: `src/io/schema.rs`

```json
{
  "status": "success",
  "entity_type": "symbol",
  "count": 3,
  "items": [...],
  "metadata": {
    "query": "parse",
    "tool": null,
    "timing_ms": 15,
    "truncated": false
  },
  "guidance": "Use 'describe' for full context"
}
```

**Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `status` | `"success"` \| `"not_found"` \| `"partial_success"` \| `"error"` | Operation outcome |
| `entity_type` | string | `symbol`, `function`, `search_result`, `impact`, etc. |
| `count` | integer | Number of items in result |
| `items` | array | Data payload (or `groups`, `results`, `item` depending on shape) |
| `metadata` | object? | Query info, timing, truncation flag |
| `guidance` | string? | AI assistant guidance |

**Data shape variants** (via `#[serde(flatten)]`):
- `{ "items": [...] }` - Simple list
- `{ "groups": { "Function": [...], "Struct": [...] } }` - Grouped by category
- `{ "results": [...] }` - Ranked with scores
- `{ "item": {...} }` - Single result

## Problems

1. **Inconsistent field names**: `system_message` vs `guidance`, `data` vs `items`
2. **Different status values**: `"success"/"error"` vs `"success"/"not_found"/"partial_success"/"error"`
3. **No unified error structure**: `JsonResponse` has `error.suggestions`, `UnifiedOutput` has none
4. **Flattened data shapes**: `UnifiedOutput` uses `#[serde(untagged)]` which makes parsing harder
5. **Missing fields**: `UnifiedOutput` lacks `code`, `message`, `exit_code`
6. **exit_code not serialized**: `UnifiedOutput` has `#[serde(skip)]` on exit_code

## Unix CLI Best Practices

Reference: [JSON Lines](https://jsonlines.org/), [jq patterns](https://stedolan.github.io/jq/manual/)

### Recommended patterns:

1. **Consistent envelope** - Same wrapper for all commands
2. **Explicit typing** - `"type": "symbol"` not inferred from context
3. **Always include exit code** - For scripting
4. **Flat data by default** - Avoid nested variants; use `"data": [...]` always
5. **Errors separate from results** - Don't mix `error` and `data` presence
6. **Pagination metadata** - `total`, `offset`, `limit` for large results
7. **Streaming support** - Consider JSON Lines for large outputs

### Example unified schema:

```json
{
  "ok": true,
  "code": "OK",
  "message": "Found 3 symbols",
  "exit_code": 0,
  "data": {
    "type": "symbols",
    "count": 3,
    "items": [...]
  },
  "meta": {
    "query": "parse",
    "timing_ms": 15,
    "version": "0.1.0"
  },
  "hint": "Use 'describe symbol_id:123' for details"
}
```

**Key changes:**
- `ok: boolean` instead of `status: string` (easier to check in scripts)
- `data.type` explicit instead of `entity_type` at root
- `data.items` always an array (no flattened variants)
- `hint` instead of `system_message`/`guidance`
- `exit_code` always present and serialized

## Alternatives to Consider

### Option A: Adopt existing standard

[JSON:API](https://jsonapi.org/):
```json
{
  "data": [
    { "type": "symbol", "id": "123", "attributes": {...} }
  ],
  "meta": { "total": 100 },
  "errors": [...]
}
```

Pros: Well-documented, tooling exists
Cons: Verbose, overkill for CLI

### Option B: Minimal envelope

```json
{
  "ok": true,
  "result": [...],
  "error": null
}
```

Pros: Simple, easy to parse
Cons: Loses metadata, guidance

### Option C: jq-friendly flat output

```json
{"type":"symbol","id":123,"name":"parse","kind":"function","file":"src/lib.rs","line":42}
{"type":"symbol","id":124,"name":"format","kind":"function","file":"src/lib.rs","line":100}
```

Pros: Streamable, easy to filter with `jq`
Cons: No envelope, no metadata per-response

## Migration Path

1. Design unified schema (this review)
2. Add `--output-format=v2` flag with new schema
3. Deprecate old schemas with warning
4. Make v2 default in next major version
5. Remove v1 schemas

## Next Steps

- [ ] Decide on schema structure
- [ ] Decide on field naming conventions
- [ ] Implement in `src/io/format.rs`
- [ ] Update all commands to use unified output
- [ ] Document final schema in docs
- [ ] Add JSON schema file for validation (`codanna.schema.json`)

## References

- [12 Factor CLI Apps](https://medium.com/@jdxcode/12-factor-cli-apps-dd3c227a0e46)
- [Command Line Interface Guidelines](https://clig.dev/)
- [JSON Lines](https://jsonlines.org/)
- [jq Manual](https://stedolan.github.io/jq/manual/)
