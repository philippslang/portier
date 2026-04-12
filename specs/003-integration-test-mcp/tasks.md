# Tasks: MCP Integration Test Suite

**Input**: Design documents from `/specs/003-integration-test-mcp/`  
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓

**Organization**: Tasks are grouped by user story so each story's test coverage can be added and verified independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different logical sections of the same file, no sequential dependency)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Exact file: `integration_test.go` (root package `portier`)

---

## Phase 1: Setup

**Purpose**: Create the test file skeleton so subsequent tasks have a place to land.

- [x] T001 Create `integration_test.go` at repo root with `package portier` declaration, blank import block, and a single build-check comment `// Integration tests for the MCP gateway`

**Checkpoint**: `go build ./...` passes with the new empty file.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared test infrastructure that every user story phase depends on. Must be complete before Phase 3.

**⚠️ CRITICAL**: All subsequent test functions depend on these helpers.

- [x] T002 Define `captured` struct and `newStub` helper in `integration_test.go` — `newStub` starts an `httptest.Server` that records method, path, rawQuery, body, and contentType into a `*captured` and always responds `HTTP 200 {}` (see plan.md Stub server pattern)
- [x] T003 Define `newTestRegistry` helper in `integration_test.go` — creates a `*Registry` using `stub.Client()`, calls `LoadSpec` for both `apis/pets.yaml` and `apis/bookstore.yaml` with `Host` overridden to `stub.URL` and `RequireConfirmation: &trueVal`, returns `(reg, stub, teardown)` so callers can `defer teardown()`
- [x] T004 Define `callCase` struct type in `integration_test.go` — fields: `name string`, `service string`, `operationID string`, `params map[string]any`, `confirmed bool`, `wantMethod string`, `wantPath string`, `wantQuery string`, `wantBody map[string]any`, `wantBlocked bool` (see data-model.md)
- [x] T005 Define `opCase` struct type in `integration_test.go` — fields: `name string`, `service string`, `tag string`, `operationID string`, `wantOpIDs []string`, `wantField string`, `wantRequired []string` (see data-model.md)

**Checkpoint**: `go vet ./...` passes; all helpers compile; no test functions exist yet so `go test ./...` passes trivially.

---

## Phase 3: User Story 1 — Read Operations Produce Correct HTTP Requests (Priority: P1) 🎯 MVP

**Goal**: Every GET operation in both specs is exercised via `CallOperation`; the captured HTTP request is asserted for correct method, URL, path parameters, and query parameters.

**Independent Test**: `go test -v -run TestCallOperation ./...` shows subtests for all 5 GET operations (listPets, getPetById, listBooks, getBookById, listReviews) passing.

- [x] T006 [US1] Write `TestCallOperation` function skeleton in `integration_test.go` — sets up registry via `newTestRegistry`, iterates table, calls `reg.CallOperation`, branches on `wantBlocked`, asserts `wantMethod`/`wantPath`/`wantQuery`/`wantBody` against `captured`; leave the table empty for now
- [x] T007 [P] [US1] Add `pets` GET table rows to `TestCallOperation` in `integration_test.go`: `listPets` (no params, wantPath `/pets`), `listPets with limit` (params `limit:5`, wantQuery `limit=5`), `listPets omit limit` (no params, wantQuery empty), `getPetById` (params `petId:"pet-42"`, wantPath `/pets/pet-42`)
- [x] T008 [P] [US1] Add `bookstore` GET table rows to `TestCallOperation` in `integration_test.go`: `listBooks` (no params), `listBooks with limit` (params `limit:3`, wantQuery `limit=3`), `listBooks with genre` (params `genre:"fiction"`, wantQuery `genre=fiction`), `listBooks omit params` (no params, wantQuery empty), `getBookById` (params `bookId:"bk-7"`, wantPath `/books/bk-7`), `listReviews` (params `bookId:"bk-7"`, wantPath `/books/bk-7/reviews`)

**Checkpoint**: `go test -v -run TestCallOperation ./...` — all GET subtests pass; no mutating subtests yet.

---

## Phase 4: User Story 2 — Mutating Operations Produce Correct HTTP Requests When Confirmed (Priority: P2)

**Goal**: Every POST/PUT/PATCH/DELETE operation is exercised twice — once with `confirmed=true` asserting the correct outbound request, and once with `confirmed=false` asserting the write gate blocks the call.

**Independent Test**: `go test -v -run TestCallOperation ./...` shows subtests for all 8 mutating operations (both confirmed and blocked variants) passing alongside the read subtests from US1.

- [x] T009 [P] [US2] Add `pets` mutating confirmed rows to `TestCallOperation` in `integration_test.go`: `createPet confirmed` (POST `/pets`, body `{name:Fido}`), `updatePet confirmed` (PUT `/pets/pet-42`, body `{name:Rex}`), `deletePet confirmed` (DELETE `/pets/pet-42`, wantBody nil)
- [x] T010 [P] [US2] Add `bookstore` mutating confirmed rows to `TestCallOperation` in `integration_test.go`: `createBook confirmed` (POST `/books`, body `{title:Dune,author:Herbert}`), `replaceBook confirmed` (PUT `/books/bk-7`, body), `patchBook confirmed` (PATCH `/books/bk-7`, wantMethod PATCH), `deleteBook confirmed` (DELETE `/books/bk-7`), `createReview confirmed` (POST `/books/bk-7/reviews`, body `{rating:5,body:Great}`)
- [x] T011 [US2] Add write-gate blocking rows to `TestCallOperation` in `integration_test.go` — one row with `confirmed:false` and `wantBlocked:true` for each of the 8 mutating operations (createPet, updatePet, deletePet, createBook, replaceBook, patchBook, deleteBook, createReview); assert `captured.called` is false and result `status` is `confirmation_required`
- [x] T012 [US2] Implement `wantBody` assertion in `TestCallOperation` in `integration_test.go` — when `wantBody` is non-nil, unmarshal `captured.body` and compare with `wantBody` using `reflect.DeepEqual`; also assert `captured.contentType` starts with `application/json`

**Checkpoint**: `go test -v -run TestCallOperation ./...` — all 34 subtests (5 GET read + optional-query variants + 8 confirmed mutating + 8 blocked) pass.

---

## Phase 5: User Story 3 — Discovery Tools Return Correct Metadata (Priority: P3)

**Goal**: `list_services`, `list_operations` (with and without tag filter), and `get_operation_detail` are exercised and asserted against the expected metadata from both specs.

**Independent Test**: `go test -v -run "TestListServices|TestListOperations|TestGetOperationDetail" ./...` — all subtests pass.

- [x] T013 [P] [US3] Write `TestListServices` in `integration_test.go` — calls `reg.ListServices()`, asserts result contains entries for both `"pets"` and `"bookstore"` service names (order-independent)
- [x] T014 [P] [US3] Write `TestListOperations` in `integration_test.go` with table: `pets no filter` (expects all 5 pet op IDs), `bookstore tag books` (expects listBooks/createBook/getBookById/replaceBook/patchBook/deleteBook, NOT listReviews or createReview), `bookstore tag reviews` (expects listReviews and createReview only), `bookstore no filter` (expects all 8 bookstore op IDs); use `reflect.DeepEqual` on sorted slices
- [x] T015 [P] [US3] Write `TestGetOperationDetail` in `integration_test.go` with table: `createPet has required name param` (assert parameters slice contains entry with `name:"name"` and `required:true`), `listPets has optional limit` (assert limit param present with `required:false`), `getPetById has responseSchema` (assert `"responseSchema"` key present in result), `createBook has requestBody` (assert `"requestBody"` key present), `patchBook confirmationRequired true` (assert `"confirmationRequired":true`), `listPets confirmationRequired false` (assert `"confirmationRequired":false`)

**Checkpoint**: `go test -v -run "TestListServices|TestListOperations|TestGetOperationDetail" ./...` — all subtests pass.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Quality gate, coverage verification, and documentation validation.

- [x] T016 Run `go vet ./...` and `go build ./...` in `integration_test.go` and fix any issues (unused imports, shadow variables, missing error checks)
- [x] T017 Verify CI-001 compliance: run `go test -v -run TestCallOperation ./...` and confirm all 13 operation IDs from data-model.md coverage map appear as subtest names; add any missing rows
- [x] T018 Verify CI-002 compliance: confirm `wantBlocked:true` rows exist for all 8 mutating operation IDs; add any missing blocking rows to `integration_test.go`
- [x] T019 [P] Validate quickstart.md commands: run `go test -v -run "TestListServices|TestListOperations|TestGetOperationDetail|TestCallOperation" ./...` and confirm all subtests pass; run `go test -race ./...` and confirm no data races

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS all user story phases
- **US1 (Phase 3)**: Depends on Phase 2; T007 and T008 can run in parallel after T006
- **US2 (Phase 4)**: Depends on Phase 3 (TestCallOperation skeleton + assertion logic); T009 and T010 can run in parallel; T011 depends on T009+T010
- **US3 (Phase 5)**: Depends on Phase 2; T013, T014, T015 are all parallel (different test functions)
- **Polish (Phase 6)**: Depends on Phases 3, 4, 5 all complete

### User Story Dependencies

- **US1 (P1)**: Can start immediately after Foundational — no dependency on US2 or US3
- **US2 (P2)**: Depends on US1 (shares `TestCallOperation`; mutating rows are added to the same table)
- **US3 (P3)**: Independent of US1 and US2 — different test functions; can start after Foundational in parallel with US1

### Within Each Phase

- T002 → T003 (registry helper needs stub helper)
- T004 and T005 can run in parallel with T002/T003 (struct definitions, no deps)
- T006 depends on T002, T003, T004
- T007 and T008 depend on T006 (table must exist), then parallel
- T009 and T010 depend on T006 (table) and T012 should follow T009/T010
- T011 depends on T009 and T010 (all blocking rows added together)
- T013, T014, T015 depend on T003 only

---

## Parallel Example: Phase 2 (Foundational)

```
# T002 and T004+T005 can be done in parallel:
Task T002: "Implement captured struct and newStub helper in integration_test.go"
Task T004: "Define callCase struct in integration_test.go"
Task T005: "Define opCase struct in integration_test.go"

# T003 runs after T002:
Task T003: "Implement newTestRegistry helper in integration_test.go"
```

## Parallel Example: Phase 5 (US3 Discovery Tools)

```
# All three test functions are independent — write in parallel:
Task T013: "Write TestListServices in integration_test.go"
Task T014: "Write TestListOperations in integration_test.go"
Task T015: "Write TestGetOperationDetail in integration_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (stub, registry helper, struct types)
3. Complete Phase 3: US1 — GET operations
4. **STOP and VALIDATE**: `go test -v -run TestCallOperation ./...` — all GET subtests green
5. Deliverable: confirmed that read operations produce correct HTTP requests

### Incremental Delivery

1. Phase 1 + 2 → infrastructure ready
2. Phase 3 (US1) → read-path correctness confirmed → demo/review
3. Phase 4 (US2) → write-gate + mutating requests confirmed → demo/review
4. Phase 5 (US3) → discovery tool metadata confirmed → full suite green
5. Phase 6 → quality gate passes → ready to merge

### Parallel Opportunity

With one developer: follow priority order (P1 → P2 → P3).  
With two developers: after Phase 2, Developer A takes US1+US2 (TestCallOperation), Developer B takes US3 (TestListServices, TestListOperations, TestGetOperationDetail) in parallel.

---

## Notes

- All tasks write to a single file (`integration_test.go`); "parallel" means logically independent sections that can be drafted without blocking each other
- `[P]` in the context of a single-file feature means the content can be authored concurrently (different table rows / different functions) and merged without conflicts
- Each user story has a clear `go test -run` filter so progress can be validated independently
- The write-gate blocking assertion (T011) MUST verify `captured.called == false`; do not rely solely on the returned status field
- The known PATCH content-type gap (plan.md Known Limitation) is documented — assert `application/json`, not `application/json-patch+json`
