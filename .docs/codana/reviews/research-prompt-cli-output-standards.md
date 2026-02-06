# Research Prompt: CLI Output Standards for Piping and Scripting

## Context

I'm designing a unified JSON output schema for a Rust CLI tool (code intelligence/indexing). The tool has multiple subcommands that output structured data. Currently two incompatible schemas exist internally. I need to unify them using industry best practices.

**Primary use cases:**
1. Piping output to `jq`, `grep`, `awk`, `sed`
2. Scripting in bash, zsh, fish
3. Consumption by other CLI tools in pipelines
4. Integration with AI assistants (MCP protocol)
5. Machine-readable output for CI/CD

**Current pain points:**
- Inconsistent envelope structure between commands
- Nested/variant data shapes hard to parse
- Exit codes not always in output
- No streaming support for large results

## Research Questions

### 1. Established Standards

Research existing standards for structured CLI output:

- **JSON Lines (NDJSON)** - When is it preferred over JSON arrays?
- **JSON:API** - Is it appropriate for CLI tools or too verbose?
- **HAL (Hypertext Application Language)** - Any CLI adoption?
- **JSend** - Simple status/data/message pattern - still recommended?
- **JSON-RPC 2.0** - For request/response patterns
- **OData JSON** - Enterprise patterns applicable to CLI?

For each, provide:
- Specification URL
- Adoption in popular CLI tools (examples)
- Pros/cons for piping workflows
- Streaming compatibility

### 2. Unix Philosophy Alignment

How do modern CLI tools balance:
- Structured output (JSON) vs plain text
- Human-readable vs machine-readable
- Single responses vs streaming
- Envelope metadata vs raw data

Research how these tools handle output:
- `gh` (GitHub CLI)
- `aws` CLI
- `gcloud` CLI
- `kubectl`
- `docker`
- `jq` (as both consumer and producer)
- `ripgrep` (`--json` flag)
- `fd`
- `bat`
- `exa`/`eza`
- `nushell` (structured data shell)

What patterns emerge? Document specific schema examples from each.

### 3. Piping Patterns

Research optimal patterns for:

**A. Output to jq:**
```bash
tool command --json | jq '.items[] | select(.type == "function")'
```
- What schema structure works best with jq filters?
- Array at root vs wrapped in envelope?
- Flat vs nested objects?

**B. Chaining CLI tools:**
```bash
tool search "query" --json | tool2 process --stdin
```
- How should tools accept piped JSON input?
- Error propagation in pipelines
- Partial failure handling

**C. Streaming large results:**
```bash
tool list --all --json | head -100
```
- JSON Lines vs JSON array for streaming
- Memory efficiency
- Interrupted output handling

**D. Mixed stdout/stderr:**
```bash
tool command --json 2>&1 | process
```
- Where should errors go? stderr or JSON?
- Progress messages in JSON mode
- Combining human and machine output

### 4. Error Handling Standards

Research best practices for:
- Error structure in JSON output
- Exit codes and their JSON representation
- Partial success (some items failed)
- Validation errors with field-level detail
- Suggestions/hints for recovery

Examples from real tools:
- How does `gh` report API errors?
- How does `kubectl` handle partial failures?
- How does `aws` structure error responses?

### 5. Pagination and Metadata

For commands returning large datasets:
- Cursor-based vs offset pagination in JSON
- Total count inclusion
- Next page tokens
- Timing/performance metadata

### 6. Versioning and Evolution

How do CLI tools evolve their JSON schema without breaking consumers?
- Schema versioning strategies
- Deprecation patterns
- Backwards compatibility approaches

### 7. AI/LLM Integration

Modern consideration: output consumed by AI assistants
- System messages/hints in output
- Context for follow-up actions
- MCP (Model Context Protocol) compatibility
- Structured data for function calling

## Deliverables

1. **Comparison matrix** of standards with ratings for:
   - Piping friendliness (1-5)
   - Streaming support (1-5)
   - Tooling ecosystem (1-5)
   - Simplicity (1-5)
   - Future-proofing (1-5)

2. **Recommended schema** with:
   - Full JSON schema definition
   - Examples for success, error, partial success, streaming
   - Rationale for each design decision

3. **Anti-patterns** to avoid with real-world examples of what goes wrong

4. **Migration guidance** for evolving existing schemas

## Constraints

- Must work with standard Unix tools (`jq`, `grep`, `head`, `tail`)
- Must support both human operators and scripts
- Must be implementable in Rust with serde
- Should not require special client libraries to parse
- Should support streaming without buffering entire response

## Output Format

Structure your research as:

```markdown
## 1. Standards Analysis
### [Standard Name]
- Specification: [URL]
- CLI Adoption: [Examples]
- Piping Score: [1-5]
- Recommendation: [Use/Avoid/Consider]

## 2. Tool Survey
### [Tool Name]
- JSON Schema: [code block]
- Notable Patterns: [list]

## 3. Recommended Schema
[Full specification]

## 4. Anti-patterns
[List with examples]

## 5. References
[URLs]
```
