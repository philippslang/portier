# Phase 1 Data Model: Search Operations Tool

No persistent storage. Three in-memory concerns: the input parameters from the MCP call, an additive field on the existing `Operation` entity, and the response envelope.

## Modified entity

### `Operation` (existing, in `registry.go`)

Add one field; everything else is unchanged.

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| `Description` | `string` | `openapi3.Operation.Description`, captured in `Registry.LoadSpec` | New. Needed so that search can match against the operation description without re-parsing the spec at query time (Constitution IV: no runtime registry mutation). Populated at load time; empty string is acceptable. Omitted from JSON where `Operation` is serialized by giving the field the same tag style as `Summary`. |

No other fields on `Operation` change. No changes to `Service` or `Registry`.

## New input entities (transient — lifetime is one tool call)

### `SearchQuery`

| Field | Type | Constraints |
|-------|------|-------------|
| `query` | `string` | **Required.** Non-empty after `strings.TrimSpace`. If empty ⇒ handler returns an MCP tool error (FR-010). Case-insensitive literal substring. No regex parsing. |
| `services` | `[]string` | Optional. Empty/absent ⇒ search all registered services. Duplicates are deduplicated server-side; unknown names are collected into the response's `unknownServices` and otherwise ignored (FR-011). |

Not a Go struct in code; these are handler-local variables extracted from `request.GetArguments()` following the existing convention in `tools.go`.

## New output entities

### `SearchResult` (one per matching operation)

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| `service` | `string` | `Service.Name` | Lets the agent pass the right `service` to `get_operation_detail`. |
| `operationId` | `string` | `Operation.OperationID` | |
| `method` | `string` | `Operation.Method` | Uppercase, matches the registry's canonical form. |
| `path` | `string` | `Operation.Path` | |
| `summary` | `string` | `Operation.Summary` | Raw summary; may be empty if the spec omits it. |
| `confirmationRequired` | `bool` | `svc.RequireConfirmation && isMutating(op.Method)` | Same computation `list_operations` uses, for parity with Constitution III. |

Deliberately **omitted** from each `SearchResult`: `description` (returning the full description per result could double or triple response size for verbose specs; the agent can call `get_operation_detail` to see it), `tags`, `parameters`, `requestBody`, `responseSchema`.

### `SearchResponse` (top-level envelope)

| Field | Type | Notes |
|-------|------|-------|
| `results` | `[]SearchResult` | Length ≤ 20. Order is stable within a single process run (Go map iteration is randomized, so iterate services in a deterministic order — e.g., sort service names — before walking operations; within a service, walk `Operations` in sorted operationId order). Cross-process determinism is not required by the spec. |
| `truncated` | `bool` | `true` iff the scan stopped because the 20-item cap was reached. |
| `unknownServices` | `[]string` | Present only when non-empty; lists filter entries that didn't correspond to a registered service. |

## State transitions

None. Search is a pure function of `(query, services, Registry.services snapshot)`. The registry is read under `RLock` exactly like `ListOperations`, and no field is mutated.

## Validation rules (consolidated)

- `query` must be non-empty after `TrimSpace` → otherwise `mcp.NewToolResultError("query is required")` (FR-010).
- `services`, when present, must be `[]any` with each element castable to `string`; non-string elements are ignored and do not fail the call (matches existing permissive parsing in `call_operation`).
- Duplicates in `services` are deduplicated via a `map[string]struct{}`.
- Unknown service names accumulate into `unknownServices`.
- Matching: `strings.Contains(strings.ToLower(field), strings.ToLower(trimmedQuery))` applied in order `Path`, `Summary`, `Description`; first hit records the operation and moves on.
- Result cap: 20 matches; further scanning stops immediately when reached.
- Visibility: iterate only over `svc.Operations` (the post-`allow_operations`, post-`ignore_headers` view) so hidden operations are structurally unreachable.
