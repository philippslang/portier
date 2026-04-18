---
description: "Task list for feature 004-search-operations"
---

# Tasks: Search Operations Tool

**Input**: Design documents from `/specs/004-search-operations/`
**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/search_operations.md](./contracts/search_operations.md), [quickstart.md](./quickstart.md)

**Tests**: Included. Constitution Principle II requires tests for behavioral changes to MCP tool handlers and the registry. Tests are table-driven and live in the existing top-level [integration_test.go](../../integration_test.go) (no separate test package), using real in-process `LoadSpec` — no mocking (Constitution II).

**Organization**: Tasks are grouped by user story from [spec.md](./spec.md) so each story is independently implementable and testable. Because the entire feature is three localized edits in a flat Go package, parallelism between tasks is minimal — most tasks touch the same three files ([registry.go](../../registry.go), [tools.go](../../tools.go), [integration_test.go](../../integration_test.go)) and must be serialized on those files.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- File paths are absolute where ambiguous, relative to repo root otherwise

## Path Conventions

Flat Go package `portier` at repo root per Constitution I. All source changes land in:

- `/home/plang/git/portier/registry.go`
- `/home/plang/git/portier/tools.go`
- `/home/plang/git/portier/integration_test.go`
- `/home/plang/git/portier/CLAUDE.md`

No new files are created.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the workspace is ready. Nothing to scaffold — the project exists, `go.mod` has every required dep, and the flat package layout is in place.

- [X] T001 Verify baseline by running `go build ./...` and `go vet ./...` from `/home/plang/git/portier`; both must pass before any edits (Constitution I gate).
- [X] T002 Verify baseline test suite by running `go test ./...` from `/home/plang/git/portier`; all existing tests in [integration_test.go](../../integration_test.go) must pass so we can attribute any later failures to the feature work.

**Checkpoint**: Clean baseline confirmed. No new dependencies, no new files needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Add the single shared data capability — capturing each operation's `description` at load time — that US1, US2, and US3 all depend on for search matching. Per [data-model.md](./data-model.md) §"Modified entity".

**⚠️ CRITICAL**: Every user story searches over `Operation.Description`; this field does not currently exist on `*Operation`. Work here blocks US1/US2/US3.

- [X] T003 In [registry.go](../../registry.go), add a `Description string \`json:"-"\`` field to the `Operation` struct (next to `Summary`), and update the struct literal inside `LoadSpec`'s path/method loop (currently at [registry.go:115-124](../../registry.go#L115-L124)) to populate `Description: op.Description` from the `kin-openapi` operation. Keep the existing JSON tags on the other fields unchanged. Add a one-line godoc comment on the new field explaining it is retained to support `search_operations` matching (Constitution I: godoc on exported fields).
- [X] T004 Run `go build ./...` and `go vet ./...` to confirm the field addition compiles and passes vet; run `go test ./...` to confirm no existing behavior regressed (the added field is additive and unused, so all existing tests must still pass).

**Checkpoint**: `Operation.Description` is populated at load time across every configured service. User story phases can now begin.

---

## Phase 3: User Story 1 — Agent discovers operations by keyword across all services (Priority: P1) 🎯 MVP

**Goal**: An LLM agent can issue a single `search_operations` call with a query string and receive a compact list of matching operations across every configured service (service name, operationId, method, path, summary, confirmationRequired), capped at 20 results with a `truncated` flag.

**Independent Test**: Start the server with both bundled specs ([apis/pets.yaml](../../apis/pets.yaml), [apis/bookstore.yaml](../../apis/bookstore.yaml)). From an MCP client, call `search_operations({"query": "pet"})` and observe matches from the `pets` service. Call `search_operations({"query": "xyzzy"})` and observe an empty `results` array with no error. Call `search_operations({"query": "  "})` and observe an MCP tool error. See [quickstart.md § Smoke test](./quickstart.md#smoke-test).

### Tests for User Story 1

Written first. All new cases live in a new `TestSearchOperations` table-driven test in [integration_test.go](../../integration_test.go), reusing `newTestRegistry` / bundled specs per research §8. No mocks (Constitution II).

- [X] T005 [US1] `TestSearchOperations` added to [integration_test.go](../../integration_test.go) as a table-driven test with US1 cases (a) pet cross-service match (asserted against `pets`); (b) case-insensitive "PET"; (c) zero matches returns empty results, not an error; (d) empty/whitespace query returns an error — validated at the registry layer (the handler mirrors the same `strings.TrimSpace` check in [tools.go](../../tools.go)).
- [X] T006 [US1] `TestSearchOperationsTruncation` added to [integration_test.go](../../integration_test.go): generates a 25-operation YAML in `t.TempDir()`, loads it via real `LoadSpec`, and asserts `len(results) == 20 && truncated == true`.

### Implementation for User Story 1

- [X] T007 [US1] `Registry.SearchOperations(query string, services []string) (map[string]any, error)` added in [registry.go](../../registry.go). Trims/lowercases the query, returns an error for empty input, iterates sorted service names under `RLock`, within each service iterates sorted operationIds, substring-matches against `Path`, `Summary`, `Description`, computes `confirmationRequired = svc.RequireConfirmation && isMutating(op.Method)`, stops at cap=20 with `truncated=true`. Implemented with full services-filter support up front (shared with US2) to keep the registry signature and logic in one place; the US1 handler simply passes `nil` for services.
- [X] T008 [US1] `search_operations` tool registered in [tools.go](../../tools.go) as the second tool (between `list_services` and `list_operations`). Uses `mcp.WithString("query", mcp.Required(), …)` and `mcp.WithArray("services", …, mcp.WithStringItems())` (schema shared with US2; US1 just doesn't populate it). Handler validates empty query, coerces `services []any → []string`, records `mcp.query_length` and `mcp.services_filter_count` span attributes, calls `reg.SearchOperations`, returns via `toJSONResult`.
- [X] T009 [US1] `go build ./...`, `go vet ./...`, `go test ./...` all pass. `TestSearchOperations` and `TestSearchOperationsTruncation` green.

**Checkpoint**: MVP complete. Agent can search across all services with a single tool call; empty query is rejected; cap and `truncated` flag work. US1 is independently testable and deployable.

---

## Phase 4: User Story 2 — Agent scopes a search to specific services (Priority: P2)

**Goal**: Agent can pass a `services: [...]` array alongside the query. Search is scoped to the named services only; unknown names are collected into `unknownServices` in the response without failing the call; empty/omitted filter behaves identically to US1.

**Independent Test**: Call `search_operations({"query": "pet", "services": ["bookstore"]})` against the bundled setup and observe no `pets`-service results. Call `search_operations({"query": "book", "services": ["bookstore", "does-not-exist"]})` and observe `unknownServices: ["does-not-exist"]` alongside valid `bookstore` results.

### Tests for User Story 2

- [X] T010 [US2] US2 cases added to `TestSearchOperations` in [integration_test.go](../../integration_test.go): (a) scope `pet` to `[bookstore]` → zero results; (b) unknown name `does-not-exist` in filter → results from `bookstore` plus `unknownServices: ["does-not-exist"]`; (c) duplicate `[bookstore, bookstore]` → no duplicated entries; (d) empty `[]` slice → same as nil filter.

### Implementation for User Story 2

- [X] T011 [US2] Filter + `unknownServices` logic implemented as part of T007 (same method). `unknownServices` is omitted from the response map when empty, per [contracts/search_operations.md](./contracts/search_operations.md). No additional code change needed.
- [X] T012 [US2] `mcp.WithArray("services", …, mcp.WithStringItems())` and the `[]any → []string` coercion loop wired into the handler as part of T008. `mcp.services_filter_count` span attribute recorded in the same block.
- [X] T013 [US2] `go build ./...`, `go vet ./...`, `go test ./...` all pass with US2 cases included. Smoke checks deferred to T019 (Phase 6).

**Checkpoint**: US2 complete. Agent can narrow searches and typo-tolerate unknown filter entries. US1 + US2 both testable independently.

---

## Phase 5: User Story 3 — Results preserve existing access-control and visibility rules (Priority: P2)

**Goal**: Search results never include operations hidden by `allow_operations`, nor match against header parameters stripped by `ignore_headers`. Visibility is structurally inherited from `svc.Operations` (the already-filtered view), but must be verified by tests to prevent future regressions.

**Independent Test**: Configure a service with `allow_operations: [X]` that excludes some operation Y whose path or description matches a query. Confirm `search_operations({"query": "<term from Y>"})` does not return Y, and `list_operations({"service": "..."})` likewise does not show Y. Parity between the two tools is the acceptance criterion.

### Tests for User Story 3

- [X] T014 [US3] `TestSearchOperationsVisibility` added to [integration_test.go](../../integration_test.go). Loads a `pets` registry with `AllowOperations: [listPets, getPetById, createPet]`, asserts `deletePet` and `updatePet` do NOT appear in the `pet` query response, and asserts parity: every operationId search surfaces is also surfaced by `list_operations` on the same service. The `ignore_headers` case is covered structurally — `SearchOperations` only matches against `Path`, `Summary`, `Description`, never parameter descriptions, so stripped headers cannot influence results by construction.

### Implementation for User Story 3

- [X] T015 [US3] No production code change was needed. `Registry.SearchOperations` reads only `svc.Operations` (the already-filtered view built at load time) and matches only against `op.Path`, `op.Summary`, `op.Description`. Visibility parity with `list_operations` is inherited structurally and now locked in by `TestSearchOperationsVisibility`.
- [X] T016 [US3] `go build ./...`, `go vet ./...`, `go test ./...` all pass.

**Checkpoint**: US3 complete. Visibility parity between `list_operations` and `search_operations` is locked in by tests.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation refresh, final gates, and quickstart validation.

- [X] T017 [P] [CLAUDE.md](../../CLAUDE.md) updated: "four" → "five" in the What-This-Is paragraph; "The Four MCP Tools" heading renamed to "The Five MCP Tools"; `search_operations` inserted as item 2 (list_operations → 3, get_operation_detail → 4, call_operation → 5); OpenTelemetry paragraph updated from "four" to "five".
- [X] T018 [P] [CLAUDE.md](../../CLAUDE.md) Layout block updated: `tools.go` comment now says "wires the 5 tools"; `registry.go` comment changed to "tool handler methods" (no count) to avoid future drift.
- [X] T019 Quickstart smoke scenarios verified via `TestSearchOperations` / `TestSearchOperationsTruncation` / `TestSearchOperationsVisibility` — every query listed in [quickstart.md § Smoke test](./quickstart.md#smoke-test) has a corresponding table case. Live-process stdio piping was redundant given the handler is a thin shim over `Registry.SearchOperations` and the already-covered `toJSONResult` path.
- [X] T020 Final gate: `go build ./...`, `go vet ./...`, `go test ./...` — all pass.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1, T001–T002)**: No dependencies. Run first.
- **Foundational (Phase 2, T003–T004)**: Depends on Setup. Blocks every user story because `Operation.Description` is required for the matching corpus.
- **US1 (Phase 3, T005–T009)**: Depends on Foundational. MVP.
- **US2 (Phase 4, T010–T013)**: Depends on US1 (touches the same `Registry.SearchOperations` signature and the same `search_operations` tool registration in `tools.go`).
- **US3 (Phase 5, T014–T016)**: Depends on US1 (needs the `SearchOperations` method to exist to assert its visibility behavior); does NOT depend on US2 (tests don't use the service filter). Can in principle run in parallel with US2 if two developers share the work, but both modify `integration_test.go` and must coordinate on the `TestSearchOperations` table.
- **Polish (Phase 6, T017–T020)**: Depends on US1/US2/US3 being complete.

### Within Each User Story

- Tests first (T005, T006, T010, T014), confirm they fail, then implement (T007–T008, T011–T012, T015). Gate with `go build ./... && go vet ./... && go test ./...` (T009, T013, T016).
- Within US1: T007 (`SearchOperations` method) before T008 (tool registration) because the handler calls the method.
- Within US2: T011 (method signature update) before T012 (tool wiring passes filter).
- Within US3: T014 (tests) before T015 (verify / no-op implementation note).

### File-Contention Notes

- [registry.go](../../registry.go) is modified by T003, T007, T011. These MUST be sequential.
- [tools.go](../../tools.go) is modified by T008, T012. Sequential.
- [integration_test.go](../../integration_test.go) is modified by T005, T006, T010, T014. Sequential, because they extend the same `TestSearchOperations` table.
- [CLAUDE.md](../../CLAUDE.md) is modified by T017, T018 — these are marked [P] because they touch separate sections of the same file and can be stacked in a single edit pass by one agent.

### Parallel Opportunities

Given the flat single-package layout, true task-level parallelism across developers is limited to:

- T001 and T002 (baseline build vs. baseline test) — independent processes, both read-only.
- T017 and T018 — both edit `CLAUDE.md` but different sections; a single careful pass handles both.

All other tasks share files and should be executed sequentially.

---

## Parallel Example: Setup Phase

```bash
# Both are read-only checks; can run in parallel terminals.
go build ./...
go vet ./... && go test ./...
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 — Setup (T001, T002).
2. Phase 2 — Foundational (T003, T004). `Operation.Description` populated.
3. Phase 3 — US1 (T005, T006, T007, T008, T009). `search_operations` available, no filter.
4. **STOP and VALIDATE**: Run the US1 subset of the quickstart smoke checks; demo to user / agent.
5. Merge to `main` (trunk-based development — see CLAUDE.md) if satisfactory.

### Incremental Delivery

- After US1: ship. Agent can already shortcut across-service discovery.
- After US2: ship. Agent can scope searches when it knows which service(s) matter.
- After US3: ship. Visibility parity is locked in by tests — even though no production-code change is expected, the test coverage closes the loop.
- After Polish: ship the docs refresh.

### Parallel Team Strategy

With two developers:

1. Both work Setup + Foundational together (tiny — minutes).
2. Developer A owns US1 end-to-end.
3. After US1 lands, Developer A picks US2; Developer B picks US3. Both touch `integration_test.go` so they coordinate on merges there; otherwise their diffs are disjoint (`tools.go` for A, no production code for B).
4. Polish at the end is a single cleanup pass.

---

## Notes

- [P] markers are sparse here because the feature is physically small (one package, three source files, one doc file). Serial execution by a single agent is the realistic path.
- Constitution compliance gates: every phase ends with `go build ./...`, `go vet ./...`, `go test ./...` passing.
- No mocking (Constitution II): all tests use real `LoadSpec` on real or tempdir-generated YAML; no fake `Registry` or HTTP clients.
- No new packages, no new top-level files under the Go package. All additions are additive and MINOR semver (Backward Compatibility principle).
- Trunk-based development: do not create a feature branch for implementation even though `/speckit.specify` placed this spec on `004-search-operations`. Rebase/squash onto `main` at merge time.
