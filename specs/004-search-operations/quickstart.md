# Quickstart: Search Operations Tool

Ships as the fifth built-in MCP tool. No configuration changes required — start Portier exactly as today, and the tool is available.

## For operators

Nothing to configure. The tool:

- Honors your existing `allow_operations` lists (hidden operations stay hidden).
- Honors your existing `ignore_headers` settings.
- Requires no new `config.yaml` keys.

Start the server as usual:

```bash
go run ./cmd/portier config.yaml
```

Check it's registered by listing tools via any MCP client — you should see five tools: `list_services`, `search_operations`, `list_operations`, `get_operation_detail`, `call_operation`.

## For agents (LLM usage pattern)

Fastest path to execute an operation by keyword:

1. `search_operations({ query: "cancel order" })` → inspect `results` for the right (service, operationId).
2. `get_operation_detail({ service: "<name>", operationId: "<id>" })` → learn the parameter schema.
3. `call_operation({ service, operationId, params, confirmed: true })` → execute (supply `confirmed` only if the operation is mutating on a service that requires confirmation).

Scope by service once you know the right service:

```json
{ "query": "status", "services": ["petstore"] }
```

If results come back with `truncated: true`, refine the query — use a more specific substring rather than paging.

## For library embedders

No API changes required. `portier.RegisterTools(mcpServer, registry)` continues to wire every built-in tool, now including `search_operations`.

If you drive the registry directly from Go, the new method is:

```go
func (r *Registry) SearchOperations(query string, services []string) (map[string]any, error)
```

Returns the envelope documented in `contracts/search_operations.md`.

## Smoke test

With the bundled `apis/pets.yaml` and `apis/bookstore.yaml`:

- `search_operations({"query": "pet"})` → several matches, all from the `pets` service.
- `search_operations({"query": "book"})` → matches from the `bookstore` service.
- `search_operations({"query": "status"})` → matches from whichever services have "status" in a path or summary.
- `search_operations({"query": "xyzzy-nonexistent"})` → `{ "results": [], "truncated": false }`.
- `search_operations({"query": "pet", "services": ["bookstore"]})` → no pet-service matches, because the filter scopes out `pets`.
- `search_operations({"query": "pet", "services": ["bookstore", "does-not-exist"]})` → `unknownServices: ["does-not-exist"]`.
- `search_operations({"query": "  "})` → MCP tool error "query is required".

## Verification against `list_operations` parity

To confirm Constitution / FR-007 compliance (search respects `allow_operations`):

1. Pick a service, add an `allow_operations` entry that excludes one operation.
2. Restart the server.
3. Observe that both `list_operations({service})` and `search_operations({query matching the excluded op})` agree — the excluded op appears in neither.
