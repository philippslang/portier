# Tasks: Replace method Field with confirmationRequired Boolean

**Input**: Design documents from `/specs/002-mutating-field/`
**Prerequisites**: plan.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓

**Note**: No spec.md — feature defined directly in plan.md. No user stories; tasks are
organized as a single implementation phase. No tests requested; no test suite exists.

**Dependency on feature 001**: `Service.RequireConfirmation bool` must be present
(merged via 001-service-level-confirmation). Verify before starting.

## Format: `[ID] [P?] Description`

- **[P]**: Can run in parallel (different files, no blocking dependency)
- All paths are at the repository root (flat Go package)

---

## Phase 2: Foundational (Blocking Prerequisite)

**Purpose**: Verify that the `Service.RequireConfirmation` field from feature 001 is
available — both output sites depend on it.

- [x] T001 Confirm `Service.RequireConfirmation bool` exists in `registry.go` and `go build ./...` passes — prerequisite for T002 and T003

**Checkpoint**: Build passes and `Service.RequireConfirmation` is present.

---

## Phase 3: Implementation — Replace method with confirmationRequired

**Goal**: Remove `method` from `list_operations` and `get_operation_detail` tool
output; replace with `confirmationRequired` computed as
`svc.RequireConfirmation && isMutating(op.Method)`.

**Verification**: Run `go build ./... && go vet ./...`. Inspect raw JSON output from
both tool calls and confirm `method` is absent and `confirmationRequired` is present
with correct values.

- [x] T002 [P] In `registry.go` `ListOperations` (~line 226), replace `"method": op.Method` with `"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method)` in the result map
- [x] T003 [P] In `registry.go` `GetOperationDetail` (~line 251), replace `"method": op.Method` with `"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method)` in the detail map
- [x] T004 [P] In `tools.go`, update the `list_operations` tool description to: `"List operations available in a service. Returns operationId, summary, confirmationRequired flag (true when the operation requires confirmed=true to execute), and tags. Optionally filter by tag."`
- [x] T005 [P] In `tools.go`, update the `get_operation_detail` tool description to: `"Get full parameter schemas, request body schema, and response schema for an operation. Also returns confirmationRequired indicating whether you must pass confirmed=true when calling this operation. Use this to understand how to call an operation before calling it."`

**Checkpoint**: `go build ./... && go vet ./...` pass. Both tool outputs contain
`confirmationRequired` and no longer contain `method`.

---

## Phase 4: Polish

- [x] T006 Run `go build ./... && go vet ./...` and confirm both pass as the final quality gate

---

## Dependencies & Execution Order

```
T001 (verify prerequisite)
  ↓
T002 (registry.go ListOperations) ‖ T003 (registry.go GetOperationDetail)
  ‖                                    ‖
T004 (tools.go list_ops desc)      T005 (tools.go detail desc)
  ↓
T006 (final build + vet)
```

T002 and T003 are in the same file (`registry.go`) — run sequentially to avoid
edit conflicts. T004 and T005 are in the same file (`tools.go`) — run sequentially.
T002/T003 and T004/T005 are in different files — those pairs can run in parallel.

### Revised safe execution order

```
T001
  ↓
T002 → T003   (registry.go — sequential, same file)
  ‖
T004 → T005   (tools.go — sequential, same file, can start after T001)
  ↓
T006
```

---

## Implementation Strategy

All tasks are part of a single atomic change — implement all of T001–T005 before
validating with T006. The change is small enough (~10 lines) to complete in one pass.

---

## Notes

- [P] markers indicate tasks in different files that can be assigned to parallel agents;
  within the same file, tasks must run sequentially
- No new files, no new structs, no config changes
- `svc` is already in scope at both call sites — no signature changes needed
- `isMutating` is already imported/available in `registry.go`
