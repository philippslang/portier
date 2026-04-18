# Feature Specification: Search Operations Tool

**Feature Branch**: `004-search-operations`
**Created**: 2026-04-18
**Status**: Draft
**Input**: User description: "Add a search tool that does grep-like search against endpoint path and and descriptions. Supports optional filter using service names, by default searches all services. Returns matching operations."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Agent discovers operations by keyword across all services (Priority: P1)

An LLM agent is asked to complete a task (e.g., "cancel the customer's most recent order") but does not yet know which service or operation to use. Instead of enumerating every service and listing every operation, the agent issues a keyword search (e.g., "cancel order") and receives a short list of operations across all configured services whose paths or descriptions match.

**Why this priority**: This is the core value of the feature. Without cross-service search, agents must walk the full progressive-discovery path (`list_services` → `list_operations` for each service → `get_operation_detail`), which wastes context tokens and slows task completion — especially as the number of configured services grows. A single search shortcut is the MVP.

**Independent Test**: Configure two or more services with overlapping vocabulary (e.g., a Petstore service with `findPetsByStatus` and an Orders service with `findOrdersByStatus`). Call the search tool with a query like "findBy status" and no service filter. Verify the response includes matching operations from both services, each with enough identifying information (service name, operation id, path, method, short description) for the agent to decide which to inspect further.

**Acceptance Scenarios**:

1. **Given** three services are configured and two contain operations whose path or description contains the substring "order", **When** the agent calls the search tool with the query "order" and no service filter, **Then** the response lists all matching operations from both services, each labeled with the service it belongs to.
2. **Given** a query that matches no operation in any configured service, **When** the agent calls the search tool, **Then** the response returns an empty result set with a clear indication that no matches were found (not an error).
3. **Given** a query that matches many operations, **When** the agent calls the search tool, **Then** the response is capped at a manageable number of matches so the agent's context is not overwhelmed.

---

### User Story 2 - Agent scopes a search to specific services (Priority: P2)

An agent already knows which service(s) are relevant to the task (for example, because the user named a product, or because a previous search revealed the right service) and wants to search only within that subset. The agent passes a list of service names alongside the query to narrow the result set and reduce noise.

**Why this priority**: Scoping is a quality-of-life refinement on top of P1. It becomes important when a user has many services configured or when unrelated services share vocabulary (e.g., "user" appearing in both a CRM and an auth service). Without P1, P2 has nothing to narrow.

**Independent Test**: Configure three services where at least two contain operations matching the same query. Call the search tool with the query plus a service filter that names only one of those services. Verify the response contains only matches from the named service and omits matches from the others.

**Acceptance Scenarios**:

1. **Given** services "petstore" and "orders" both contain operations matching the query "status", **When** the agent calls the search tool with the query "status" and a service filter listing only "petstore", **Then** the response contains only petstore operations.
2. **Given** the agent provides a service filter that includes a name that is not configured, **When** the search runs, **Then** the response still returns matches from any valid names in the filter and communicates that the unknown name was ignored — without failing the whole call.
3. **Given** the agent provides an empty or omitted service filter, **When** the search runs, **Then** the tool behaves as if all configured services were selected.

---

### User Story 3 - Results preserve existing access-control and visibility rules (Priority: P2)

Portier already enforces per-service allow lists and header-stripping rules so that LLM agents only see the surface area the operator has approved. The search tool must respect these same rules: operations that are not exposed by `list_operations` must not appear in search results.

**Why this priority**: Matches P2 because it is essential for any deployment that uses `allow_operations` today, but the feature remains usable for operators with no allow lists configured (where the rule is a no-op). Violating this would turn search into a covert discovery channel and break the operator's trust model, so it must ship with the tool from day one — but it is a correctness constraint layered onto P1/P2 rather than a standalone user journey.

**Independent Test**: Configure a service with an `allow_operations` list that explicitly hides an operation whose path or description would otherwise match a query. Call the search tool with that query. Verify the hidden operation does not appear in the results, mirroring the behaviour of `list_operations` for the same service.

**Acceptance Scenarios**:

1. **Given** a service has an `allow_operations` list that excludes operation X, **When** the agent searches for a term that would match operation X, **Then** operation X is not returned.
2. **Given** a service's ignored-header configuration hides a header parameter, **When** a query would match only because of that hidden header's description, **Then** that operation is not returned on the basis of the hidden header alone.

---

### Edge Cases

- **Empty query**: If the agent provides an empty or whitespace-only query, the tool returns an error explaining that a query is required rather than returning every operation across every service.
- **Case sensitivity**: Matching is case-insensitive by default so that a query of "PET" finds a path containing "/pet".
- **Special characters in query**: Queries containing regex metacharacters (e.g., `.`, `*`, `?`) are treated as literal substrings, so a stray character does not explode into a catch-all match.
- **Very long queries or very large specs**: The tool enforces a reasonable upper bound on the number of matches returned, so pathological queries and very large OpenAPI specs cannot cause unbounded response sizes.
- **Multiple matches in one operation**: A single operation is returned at most once, even if its path and multiple description fields all match.
- **Duplicate service names in filter**: Duplicates in the service filter do not cause duplicated results.
- **Unknown service name in filter**: Unknown names are skipped (and reported as ignored) without failing the whole call, so an agent's typo does not block legitimate matches.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST expose a new MCP tool, named `search_operations`, alongside the existing four tools.
- **FR-002**: The tool MUST accept a required free-text `query` parameter and MUST match it against each operation's path and its descriptive text (operation summary and description, at minimum).
- **FR-003**: The tool MUST support an optional list-of-service-names filter; when present, only operations from those services are considered; when absent or empty, all configured services are considered.
- **FR-004**: Matching MUST be case-insensitive and MUST treat the query as a literal substring (not as a regular expression), so that typical grep-like user input behaves predictably.
- **FR-005**: The tool MUST return each matching operation with enough identifying information for the agent to call `get_operation_detail` next — specifically: service name, operation id, HTTP method, path, and a short description (summary or first line of description).
- **FR-006**: The tool MUST return results without duplicates: one entry per matching (service, operationId) pair regardless of how many fields matched.
- **FR-007**: The tool MUST respect the same visibility rules as `list_operations`: operations excluded by the service's `allow_operations` list MUST NOT appear in search results; operations from services not listed in the server configuration MUST NOT appear.
- **FR-008**: The tool MUST cap the number of results returned to keep LLM context usage manageable, and MUST communicate (within the response) when results were truncated so the agent knows to refine its query.
- **FR-009**: The tool MUST return an empty result set (not an error) when the query is valid but matches nothing.
- **FR-010**: The tool MUST return a clear, agent-readable error when the query is missing or empty.
- **FR-011**: Unknown service names supplied in the filter MUST NOT cause the call to fail; the tool MUST proceed with the known names and MUST communicate which supplied names were unknown.
- **FR-012**: The tool's description and parameter schema, as exposed to the LLM, MUST make it clear that this is a discovery tool (not an execution tool) and that it does not call any upstream API.
- **FR-013**: The tool MUST be read-only and MUST NOT require the `confirmed` write-gate parameter used by `call_operation`.
- **FR-014**: The tool MUST be instrumented with the same telemetry pattern as the other four tools, so operators can observe its usage alongside them.

### Key Entities *(include if feature involves data)*

- **SearchQuery**: The free-text string the agent is searching for. Treated as a literal, case-insensitive substring.
- **ServiceFilter**: An optional list of service names that scopes the search. Empty/omitted means "all configured services."
- **SearchResult**: A single matching operation summary. Carries service name, operation id, HTTP method, path, and a short description — enough for the agent to choose whether to call `get_operation_detail` next. Does not include full request/response schemas.
- **SearchResponse**: The envelope returned to the agent. Contains the list of `SearchResult` entries, a flag indicating whether results were truncated, and a list of any service names from the filter that were not recognized.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a deployment with at least five configured services, an agent can locate a specific operation by keyword in a single tool call, instead of the multi-step progressive-discovery walk that requires one call per service.
- **SC-002**: When the agent's query matches operations across multiple services, at least 95% of relevant matches (path or description contains the query substring, case-insensitive) are returned in one call, up to the truncation cap.
- **SC-003**: Operations hidden by `allow_operations` never appear in search results — verified by an operator exercising both `list_operations` and `search_operations` against the same configuration and confirming parity of visible operation sets.
- **SC-004**: The response for a typical query (tens of matches) stays within a size that an LLM agent can consume in one turn — roughly an order of magnitude smaller than the equivalent `list_operations`-per-service walk.
- **SC-005**: Adding the new tool does not regress the behaviour of the existing four tools: `list_services`, `list_operations`, `get_operation_detail`, and `call_operation` continue to return the same results for the same inputs.

## Assumptions

- Substring matching (grep-like) is sufficient for v1. Full regular-expression or fuzzy matching can be added later if agents need it; the parameter schema will not foreclose that extension.
- "Descriptions" means the OpenAPI `summary` and `description` fields on each operation. Per-parameter descriptions and tag descriptions are out of scope for v1 matching to keep the result signal high and the implementation simple.
- Results are unranked in v1 — the tool returns matches in a stable but unspecified order (e.g., configuration / discovery order). Relevance ranking is a future enhancement.
- The truncation cap for results is a reasonable default (similar in spirit to the 20-item response truncation already used by `call_operation`) and does not need to be configurable in v1.
- The tool is exposed on every transport the server already supports (streamable HTTP and stdio) with no transport-specific behaviour.
- Existing configuration shape (`config.yaml` services, `allow_operations`, `ignore_headers`) is sufficient; no new configuration keys are required for v1.
- The feature is purely additive at the MCP surface — no changes to the semantics of the existing four tools.
