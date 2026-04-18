# Implementation Plan: Search Operations Tool

**Branch**: This project uses trunk-based development. Do not create feature branches.
**Input**: Feature specification from `/specs/004-search-operations/spec.md`

## Summary

Add a fifth MCP tool, `search_operations`, that performs a case-insensitive substring search over every registered operation's path, `summary`, and `description`, optionally filtered by a list of service names. Returns a compact list of matches (service, operationId, method, path, short summary) so an LLM agent can jump straight to `get_operation_detail` without walking `list_services → list_operations` per service. The tool is read-only, respects the existing `allow_operations` visibility rules (results are derived from the already-filtered in-memory registry), and truncates at a fixed cap consistent with the existing 20-item array truncation to keep response size predictable.

## Technical Context

**Language/Version**: Go 1.25 (module `github.com/philippslang/portier`, toolchain pinned via `go.mod`)
**Primary Dependencies**: `github.com/mark3labs/mcp-go` (MCP server + tool registration), `github.com/getkin/kin-openapi/openapi3` (already-parsed operations on `*Operation`), `go.opentelemetry.io/otel` (span instrumentation via existing `withTracing` middleware)
**Storage**: N/A — in-memory `Registry` only; no new persistence
**Testing**: `go test ./...` with stdlib `net/http/httptest`; new behavior exercised in the existing top-level `integration_test.go` file (no separate test package)
**Target Platform**: Linux server / container; also runs as stdio transport for Claude Desktop
**Project Type**: Go library + CLI (`cmd/portier/`). Flat package layout per Constitution Principle I.
**Performance Goals**: Search is O(N × M) where N = total operations across filtered services and M = query length; for any realistic deployment (N ≤ a few thousand) this is a single pass over already-loaded in-memory strings and completes in well under 1 ms — dominated by MCP serialization, not matching.
**Constraints**: MUST not raise the existing 50 ms p95 overhead budget (Constitution IV). MUST not require runtime mutation of the `Registry` (write lock only at startup). MUST not add latency to the existing four tools when disabled — `search_operations` is additive so this is trivially satisfied. Result set capped at 20 entries (matches existing array-truncation convention) to keep LLM context predictable.
**Scale/Scope**: Typical deployments configure 1–10 services with tens to low-hundreds of operations each. One new tool, one new `Registry` method, one new `RegisterTools` block, one new entry in the agent-context doc, table-driven tests added to `integration_test.go`.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Code Quality | PASS | Stays in flat package (no sub-packages). New work is one exported method on `Registry`, one block in `RegisterTools`, one godoc comment on each. `go build ./...` and `go vet ./...` are the gate. |
| II. Testing Standards | PASS | Behavioral change ⇒ table-driven tests added to existing `integration_test.go` using the same `newTestRegistry`/stub pattern; no mocking of registry or HTTP. No new perf-sensitive path (substring scan over in-memory strings), so no new benchmark is required. |
| III. Agent Interface Consistency | PASS | Additive: the existing four tools are untouched. New tool name `search_operations` matches the existing `verb_noun` pattern. Progressive discovery is preserved (search is a *shortcut into* discovery, not a replacement for `get_operation_detail` → `call_operation`). Result shape mirrors `list_operations` entries plus a `service` field. |
| IV. Performance Requirements | PASS | No upstream HTTP involved — pure in-memory scan. 20-item truncation cap aligned with existing convention. No change to flattening depth, no runtime registry mutation. `withTracing` is the same middleware used by other tools, inheriting the no-op-tracer guarantee. |
| Safety & Trust | PASS | Read-only, no write gate needed (FR-013). Does not expose static/auth headers (never matched, never returned). Does not widen visibility: results are drawn from `Registry.services[*].Operations`, which is already filtered by `allow_operations` at load time. `ignore_headers` similarly pre-strips header params, so hidden-header text is not reachable. |
| Backward Compatibility & Versioning | PASS | No config schema changes. No changes to existing public Go API signatures — one new exported method `Registry.SearchOperations` is added. Change is MINOR semver (new non-breaking capability). |

**Result**: All gates PASS. No entries in Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/004-search-operations/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── search_operations.md   # MCP tool contract (input/output schema)
├── checklists/
│   └── requirements.md  # Spec quality checklist (from /speckit.specify)
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
# Flat package layout (Constitution I)
registry.go           # + Registry.SearchOperations method
tools.go              # + search_operations tool registration inside RegisterTools
integration_test.go   # + TestSearchOperations (table-driven)
CLAUDE.md             # + brief mention of the 5th tool under "The Five MCP Tools"

# Unchanged
doc.go config.go schema.go telemetry.go server.go util.go
cmd/portier/main.go
apis/pets.yaml apis/bookstore.yaml
```

**Structure Decision**: Single flat Go package `portier` at repo root, per Constitution Principle I ("The package layout MUST remain flat (no sub-packages) unless a new sub-package carries a clearly independent, reusable purpose"). The feature is three localized diffs — one `Registry` method, one tool registration, one set of table-driven tests — plus a one-line update to `CLAUDE.md`. No new files in the Go package are needed.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No violations. Table intentionally empty.
