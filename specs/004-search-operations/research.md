# Phase 0 Research: Search Operations Tool

Purpose: resolve every Technical-Context decision point for `search_operations` before design. All items below are decisions, not open questions — no `NEEDS CLARIFICATION` markers remain.

## 1. Matching algorithm

**Decision**: Plain, case-insensitive substring match using `strings.Contains(strings.ToLower(field), strings.ToLower(query))`, applied independently to the operation's `path`, `summary`, and `description`. First field to match short-circuits for that operation; each operation is returned at most once.

**Rationale**:
- Spec FR-004 mandates case-insensitive literal substring matching; Assumptions explicitly defer regex/fuzzy to a later version.
- Stdlib only — no new dependency, which matters because `go.mod` is already dense with OpenAPI/OTel/MCP deps and Constitution I discourages unnecessary abstraction.
- At expected scale (≤ a few thousand operations, query length ≤ ~100 chars), a naive scan is orders of magnitude faster than the LLM round-trip that will consume the result. No indexing is warranted.
- Short-circuiting on first-field match keeps the inner loop tight and avoids duplicate bookkeeping (FR-006).

**Alternatives considered**:
- *Regex* — rejected for v1 (spec Assumption: regex reserved for later). Also: agents often include regex metacharacters unintentionally, producing confusing matches.
- *Fuzzy / Levenshtein / trigram* — rejected: adds a dependency, hides false positives, and the LLM can re-phrase its query for free. Worth revisiting only if feedback shows agents struggling with near-miss queries.
- *Precomputed lowercase index on `Operation`* — rejected for v1: the lowercase path/summary/description strings would double per-operation memory for a negligible CPU win (the scan is already sub-millisecond). If profiling ever shows this on a hot path, it's a trivial follow-up.

## 2. Fields searched

**Decision**: `Operation.Path` + `Operation.Summary` + the OpenAPI operation `description` field. The `description` field is not currently stored on `Operation`, so add a `Description string` field to `Operation` and populate it in `Registry.LoadSpec` from `op.Description`.

**Rationale**:
- Spec FR-002 requires matching against "path and descriptive text (operation summary and description, at minimum)".
- `Summary` is already on `Operation`; `Description` is not. Adding one string per operation is a trivial memory cost (bytes per operation) and keeps the `Registry` the single source of truth for search input — no reaching back into `openapi3.T` at query time.
- Parameter descriptions, tag descriptions, and request/response-body descriptions are **excluded** from v1 matching per the spec's Assumptions section ("keep the result signal high and the implementation simple"). Not adding them means an agent's query won't match on, say, a header description the agent can't even see after `ignore_headers` stripping — which is the correct behavior for FR-007.

**Alternatives considered**:
- *Store only a pre-joined `searchCorpus` string per operation* — slightly faster scan, but obscures which field matched if we ever want to report that in the response. Keep fields separate; join at search time if needed.
- *Re-parse the OpenAPI doc on each search* — rejected: violates Constitution IV ("No runtime mutation of the registry… write locks are taken only at startup") in spirit and adds latency.

## 3. Service-filter semantics

**Decision**: Optional `[]string` parameter. Empty or omitted ⇒ search all services. Non-empty ⇒ only operations from services whose names appear in the list; unknown names are silently skipped but accumulated into a response field `unknownServices` so the agent knows its typo didn't match. Duplicate names are deduplicated server-side.

**Rationale**:
- Spec FR-003, FR-011 and US2 scenario 2 directly prescribe this behavior.
- Accumulating unknowns into the response (instead of failing the call) is consistent with how the MCP surface treats agent input elsewhere — the write gate returns a structured prompt rather than erroring, and `list_operations` with an unknown tag returns an empty list rather than an error.
- Using a `map[string]struct{}` set built once per call for O(1) membership checks matches the pattern in `LoadSpec` (`allowSet`, `ignoreHeaders`).

**Alternatives considered**:
- *Single service string* (matching `list_operations`) — rejected: US2 explicitly passes a *list* of services, e.g., an agent scoping to both "petstore" and "orders" after a prior broad search.
- *Comma-separated string* — rejected: MCP/JSON-Schema natively supports string arrays via `mcp.WithArray`; a delimited string would force agents to learn a second encoding.
- *Fail on any unknown service* — rejected: FR-011 forbids this; a single typo from the agent would throw away valid results.

## 4. MCP tool registration — array parameter

**Decision**: Register the tool with `mcp.NewTool("search_operations", …)` using `mcp.WithString("query", mcp.Required(), …)` and `mcp.WithArray("services", mcp.Description(…), mcp.WithStringItems())` for the optional filter. Extract in the handler with `args["services"].([]any)` and coerce each element to string.

**Rationale**:
- `mcp-go` v0.47.1 (already in `go.mod`) exposes `WithArray` and `WithStringItems` helpers, which emit correct JSON-Schema (`type: array, items: {type: string}`) so LLMs know to pass an array of strings.
- The `[]any → []string` coercion pattern is standard in `mcp-go` handlers because the MCP JSON-RPC layer decodes into `any`. This matches how existing handlers extract `params map[string]any` in `call_operation`.
- Tool description phrasing will emphasize: *read-only discovery*, *case-insensitive substring*, *optional service filter*, *returns compact summaries, not full schemas* — satisfying FR-012.

**Alternatives considered**:
- *Pass services as a comma-separated `mcp.WithString`* — rejected (see §3).
- *Use `mcp.WithObject` with a nested schema* — overkill; no nested structure is needed and it makes the LLM parameter schema harder to skim.

## 5. Result cap and truncation signal

**Decision**: Hard-code the cap to **20** matches — identical to the array-response truncation cap used by `call_operation` — and expose a `truncated bool` + `totalCount int` alongside the `results` slice in the response envelope. The `Registry.SearchOperations` method stops walking once 20 matches are found; it does **not** compute a precise `totalCount` in the over-cap case (it returns `20` and `truncated: true`), because the agent's correct next move is to refine its query, not to paginate.

**Rationale**:
- Spec FR-008 requires a cap and a truncation signal; 20 is the existing convention (Constitution IV, `util.go:truncateResponse`).
- Short-circuiting at 20 keeps worst-case work bounded even for pathological deployments with tens of thousands of operations.
- Returning exactly `20 / truncated: true` instead of a precise count is consistent with the `call_operation` response shape (`totalCount`, `returnedCount`, `items`, `truncated`) where we *do* know the exact length because the upstream array is fully received — here we deliberately don't pay to keep scanning past 20.

**Alternatives considered**:
- *Configurable cap* — rejected: spec Assumptions explicitly defer this ("does not need to be configurable in v1"). Adding a config key would change `config.yaml` semantics and trigger the Backward Compatibility principle's pointer-type rule for no user benefit.
- *Walk all matches, then cap* — wastes work on the tail for no information gain; the agent's remedy is refinement, not pagination.

## 6. Response shape

**Decision**: Return `map[string]any` with three top-level keys:

- `results`: `[]map[string]any`, each entry carrying `service`, `operationId`, `method`, `path`, `summary`, plus the same `confirmationRequired` flag exposed by `list_operations` (for free — already computable from `svc.RequireConfirmation && isMutating(method)`).
- `truncated`: `bool` — true iff the cap was reached.
- `unknownServices`: `[]string` — names from the filter that weren't registered; omitted when empty (nil slice marshals away cleanly via the MCP `json.Marshal` path, but we'll explicitly not include the key when len == 0 to keep the response tidy).

**Rationale**:
- Mirrors existing `list_operations` entry shape so agents carry over intuition (Constitution III — consistency).
- Including `confirmationRequired` up front means an agent can decide from the search hit alone whether the operation will later require `confirmed=true`, shaving another round trip.
- Top-level envelope distinguishes metadata (`truncated`, `unknownServices`) from data (`results`), rather than burying flags inside each entry.

**Alternatives considered**:
- *Return bare `[]map[string]any` like `list_operations`* — rejected: can't carry truncation or unknown-service signals without ambiguity.
- *Include full flattened schemas in results* — rejected: defeats the "compact, then detail" design (FR-005) and blows up response size.

## 7. Telemetry

**Decision**: Register the handler with the existing `withTracing("search_operations", …)` middleware and add span attributes `mcp.query_length` (int) and `mcp.services_filter_count` (int). Do **not** add the raw query as an attribute — query strings can contain arbitrary agent text that we don't want in traces by default.

**Rationale**:
- Constitution IV requires the no-op-tracer path to be exercised; `withTracing` already provides that, so using it gives the guarantee for free.
- Length + filter-count give operators enough signal to spot anomalies (e.g., agents hammering the tool with 1-char queries) without logging potentially sensitive agent intent.

**Alternatives considered**:
- *Record the full query* — rejected on privacy/safety grounds; operators can opt in later if they need it for debugging.
- *Roll a bespoke span* — rejected: reuses-the-middleware keeps Principle III/IV consistency and avoids divergent span-name conventions.

## 8. Test strategy

**Decision**: Extend `integration_test.go` with a `TestSearchOperations` table-driven test that reuses `newTestRegistry` (two services: `pets`, `bookstore` — both already bundled). Cases cover:

- Cross-service substring match (US1 scenario 1).
- Case-insensitive matching.
- Empty query ⇒ error.
- Zero matches ⇒ empty `results`, no error.
- Service filter scoping to one service (US2 scenario 1).
- Unknown service name ⇒ listed in `unknownServices`, valid names still searched (US2 scenario 2).
- Duplicate service names in filter ⇒ no duplicate results.
- `allow_operations` hides an operation whose path would match ⇒ not returned (US3 scenario 1). Add a third `ServiceConfig` variant in the test helper or override the helper locally for this case.
- Truncation at 20: construct a synthetic service spec on the fly (or rely on `pets.yaml` if it has enough operations; otherwise widen query to match many). If neither spec yields ≥ 20 hits for a single substring, the cap is instead verified via a dedicated small test that calls `SearchOperations` directly on a `Registry` loaded with a single spec containing 25 operations (built programmatically against a temp YAML file or, if simpler, by registering synthetic `Operation` entries directly — keeping in mind Constitution II forbids mocking).

**Rationale**:
- Matches the existing test style and file layout (no test package changes).
- Covers every FR at least once.
- No mocks: tests load real specs through `LoadSpec` and exercise the real `Registry.SearchOperations`. The HTTP stub isn't needed since search doesn't touch the network.

**Alternatives considered**:
- *New `search_test.go` file* — acceptable but unnecessary; the existing integration test file already scopes related behaviors and the Constitution favors cohesion over file-count-based organization.
- *Benchmark* — deferred: the spec doesn't surface a performance goal beyond the blanket 50 ms overhead budget and the scan is trivially fast. If future agents complain about latency, revisit.

## 9. CLAUDE.md update

**Decision**: Change the "The Four MCP Tools" heading to "The Five MCP Tools" and insert `search_operations` between items 1 (`list_services`) and 2 (`list_operations`) — because semantically it's a broad-discovery shortcut that sits at the same layer as `list_services`, and placing it at position 2 keeps the progressive-narrowing reading order (services/search → operations → detail → call).

**Rationale**:
- `CLAUDE.md` is the single-source-of-truth overview for agents working in this repo (per the repo's in-file doc). Keeping it in sync is cheap and avoids confusion.
- `update-agent-context.sh` is invoked by the plan workflow and will merge the "Recent Changes" / "Active Technologies" footer; this §9 decision covers the manual doc body update too.

## Output

`research.md` covers every decision dimension called out by the plan template. No `NEEDS CLARIFICATION` markers remain.
