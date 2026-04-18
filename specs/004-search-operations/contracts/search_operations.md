# Tool Contract: `search_operations`

Fifth MCP tool, registered by `portier.RegisterTools` alongside the existing four. Read-only. No write-gate parameter.

## Agent-facing description

> Search operations across all registered API services by a case-insensitive substring. Matches the operation's path, summary, and description. Returns a compact list of matches — service name, operationId, method, path, summary, and whether calling it will require confirmation — so you can jump straight to `get_operation_detail` without listing every service. Optionally filter to a subset of services. Discovery only: this tool does not call any upstream API.

## Input schema (JSON Schema — as emitted by `mcp-go`)

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Case-insensitive substring to match against operation path, summary, and description. Required and must be non-empty."
    },
    "services": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Optional list of service names to scope the search. When omitted or empty, all registered services are searched. Unknown names are reported in the response's unknownServices field rather than causing an error."
    }
  },
  "required": ["query"]
}
```

## Output schema

On success, a single JSON text content block whose payload is:

```json
{
  "results": [
    {
      "service": "petstore",
      "operationId": "findPetsByStatus",
      "method": "GET",
      "path": "/pet/findByStatus",
      "summary": "Finds Pets by status",
      "confirmationRequired": false
    }
  ],
  "truncated": false,
  "unknownServices": ["typo-service"]
}
```

Field notes:

- `results`: 0–20 entries. Empty array when nothing matches (not an error).
- `truncated`: `true` iff the 20-result cap was reached; the agent should narrow its query.
- `unknownServices`: present only when at least one supplied filter name was unrecognised; absent otherwise (not emitted as `[]` or `null`).

On invalid input, returns an MCP tool error (via `mcp.NewToolResultError`) with a short, agent-readable message:

- Empty or whitespace-only `query` → `"query is required"`.

## Ordering

Results appear in a stable but unspecified order. Implementation sorts service names alphabetically and, within each service, sorts matching operationIds alphabetically. Agents MUST NOT rely on a specific ranking — the contract provides discoverability, not relevance ranking.

## Visibility guarantees

- Operations excluded from a service's `allow_operations` list never appear.
- Services not present in `config.yaml` never appear.
- Static auth headers (`ServiceConfig.Headers`) are not matched against and not returned — they are server-side only and never enter the search corpus.
- Headers stripped by `ignore_headers` are not matched against (they aren't in `Operation.Parameters` after load).

## Non-goals (explicit)

- No regex, glob, or fuzzy matching. Metacharacters are treated literally.
- No ranking or scoring.
- No pagination (truncation is a signal to refine the query).
- No upstream HTTP traffic — this is a pure registry read.
- No schema/parameter details in the response; use `get_operation_detail` for those.

## Telemetry

Invocation is wrapped in the shared `withTracing("search_operations", …)` middleware. Span attributes added: `mcp.query_length` (int), `mcp.services_filter_count` (int). The raw query text is **not** added as an attribute.

## Example traces (illustrative)

**Broad search, no filter:**
- Input: `{"query": "pet"}`
- Output shape: `results` with up to 20 matches across all services, `truncated` set appropriately, `unknownServices` absent.

**Scoped search with an unknown service name:**
- Input: `{"query": "book", "services": ["bookstore", "orders"]}` where `orders` is not configured.
- Output: `results` from `bookstore` only, `unknownServices: ["orders"]`.

**Empty match:**
- Input: `{"query": "xyzzy"}`
- Output: `{"results": [], "truncated": false}`.

**Empty query:**
- Input: `{"query": "   "}`
- Output: MCP tool error `"query is required"`.
