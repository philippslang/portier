# Tasks: Service-Level Confirmation Config

**Input**: Design documents from `/specs/001-service-level-confirmation/`  
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓

**Tests**: No test tasks — no test suite exists in this project (noted in CLAUDE.md).

**Organization**: Tasks grouped by user story. All 4 changed files are in the flat package root; no new files or directories are needed.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no blocking dependency)
- **[Story]**: User story this task serves
- All paths are at the repository root (flat Go package, no `src/`)

---

## Phase 2: Foundational (Blocking Prerequisite)

**Purpose**: Add the `*bool` field to `ServiceConfig`. Every downstream change depends on this field existing in the config struct.

**⚠️ CRITICAL**: Phases 3, 4, and 5 cannot start until this phase is complete.

- [x] T001 Add `RequireConfirmation *bool` field (yaml tag `require_confirmation,omitempty`) to `ServiceConfig` in `config.go` after the `Headers` field, with a doc comment explaining nil=inherit, true=require, false=disable

**Checkpoint**: `go build ./...` passes — new field is present and YAML-compatible.

---

## Phase 3: User Story 1 - Disable Confirmation for a Trusted Service (Priority: P1) 🎯 MVP

**Goal**: Two services in one config with different `require_confirmation` values each enforce their own write-gate behavior independently.

**Independent Test**: Start the gateway with a config containing `require_confirmation: false` on one service and `require_confirmation: true` on another. Issue a POST without `confirmed=true` to each service; only the `false` service executes.

### Implementation for User Story 1

- [x] T002 [P] [US1] Add `RequireConfirmation bool` field to the `Service` struct in `registry.go` with a doc comment ("resolved effective value; true = write gate active")
- [x] T003 [P] [US1] Remove `writeGate bool` parameter from `RegisterTools` in `tools.go`, update the function signature and its godoc comment
- [x] T004 [US1] In `registry.go` `LoadSpec`, resolve `RequireConfirmation` from `cfg.RequireConfirmation`: if nil default to `true`, otherwise dereference the pointer — store the result in `svc.RequireConfirmation`
- [x] T005 [US1] In `registry.go` `CallOperation`, update the write gate predicate from `isMutating(op.Method) && !confirmed` to `svc.RequireConfirmation && isMutating(op.Method) && !confirmed`
- [x] T006 [US1] In `tools.go` `RegisterTools` handler, remove the `writeGate` conditional blocks: always include the `confirmed` bool parameter in `callToolOpts`, always use the confirmation-aware tool description, always read `confirmed` from args with `confirmed, _ := args["confirmed"].(bool)`

**Checkpoint**: `go build ./...` passes. With two services explicitly set to `true`/`false`, each enforces its own gate.

---

## Phase 4: User Story 2 - Backward-Compatible Default Behavior (Priority: P2)

**Goal**: A config file with no per-service `require_confirmation` field produces identical behavior to the current version — confirmation required by default — without any migration.

**Independent Test**: Start the gateway using the existing `config.yaml` (unchanged, no per-service field). Issue a POST without `confirmed=true` and confirm it still returns a confirmation prompt.

### Implementation for User Story 2

- [x] T007 [US2] In `server.go` `NewServer`, before each `reg.LoadSpec(svcCfg)` call, add a nil-check: if `svcCfg.RequireConfirmation == nil`, create a local copy of the server default (`v := cfg.Server.RequireConfirmation; svcCfg.RequireConfirmation = &v`) so the service inherits the server setting
- [x] T008 [US2] In `server.go`, update the `RegisterTools` call from `RegisterTools(s, reg, cfg.Server.RequireConfirmation)` to `RegisterTools(s, reg)` (remove the now-deleted third argument)

**Checkpoint**: `go build ./...` passes. Existing `config.yaml` with no per-service field continues to enforce confirmation on all services.

---

## Phase 5: User Story 3 - Per-Service Override of Server Default (Priority: P3)

**Goal**: A per-service `require_confirmation: true` overrides a server-level `require_confirmation: false` for that service only.

**Independent Test**: Set `require_confirmation: false` at server level, set `require_confirmation: true` on one service. Confirm that service still returns a confirmation prompt while others execute immediately.

*No new implementation code is required for this story — it is fully delivered by the Phase 3 + Phase 4 changes. The `*bool` field allows explicit `true` to survive the inheritance logic in `NewServer` (only nil fields are overwritten). This phase is a verification checkpoint.*

- [x] T009 [US3] Verify the override scenario manually or by code review: trace the value of `svcCfg.RequireConfirmation` through `NewServer` for a service that explicitly sets `require_confirmation: true` when the server default is `false` — confirm the explicit value is preserved (non-nil pointer is not overwritten)

**Checkpoint**: All three user stories are independently functional. `go build ./...` passes.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation update so operators know the new field exists.

- [x] T010 [P] Add a comment to the relevant service entry in `config.yaml` documenting the new optional `require_confirmation` field (e.g., `# require_confirmation: false  # optional: override server default for this service`)
- [x] T011 [P] Review and update the `RegisterTools` godoc in `tools.go` to remove all references to the deleted `writeGate` parameter and accurately describe the current per-service behavior

---

## Dependencies & Execution Order

### Phase Dependencies

- **Foundational (Phase 2)**: No dependencies — start immediately
- **User Story 1 (Phase 3)**: Depends on Phase 2 (T001 must be complete)
- **User Story 2 (Phase 4)**: Depends on Phase 2 (T001) and Phase 3 (T003 removes `writeGate`, T008 updates the call site)
- **User Story 3 (Phase 5)**: Depends on Phase 3 and Phase 4 (verification only)
- **Polish (Phase 6)**: Depends on all story phases complete

### Within Phase 3

- T002 and T003 touch different files (`registry.go` vs `tools.go`) — run in parallel
- T004 depends on T002 (same file, sequential)
- T005 depends on T004 (same file, sequential)
- T006 depends on T003 (same file, sequential)

### Within Phase 4

- T007 and T008 are in the same file (`server.go`) — run sequentially

### Parallel Opportunities

```
Phase 2:  T001 (config.go)
               ↓
Phase 3:  T002 (registry.go) ‖ T003 (tools.go)   ← parallel start
               ↓                      ↓
          T004 (registry.go)      T006 (tools.go)
               ↓
          T005 (registry.go)
               ↓
Phase 4:  T007 → T008 (server.go)
               ↓
Phase 5:  T009 (verification)
               ↓
Phase 6:  T010 (config.yaml) ‖ T011 (tools.go)   ← parallel
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 2: T001
2. Complete Phase 3: T002 → T004 → T005 (registry changes) in parallel with T003 → T006 (tools changes)
3. **STOP and VALIDATE**: Two services with explicit `true`/`false` settings work correctly
4. Two services with different confirmation settings are now independently testable

### Incremental Delivery

1. Phase 2 + Phase 3 → US1 works (explicit per-service config)
2. Phase 4 → US2 works (backward compat / inheritance)
3. Phase 5 → US3 verified (override direction also works)
4. Phase 6 → Docs updated

---

## Notes

- [P] tasks operate on different files — no merge conflicts
- No new files or directories are created
- The `RegisterTools` signature change (removing `writeGate`) is the only breaking public API change
- Any external caller using `RegisterTools` with 3 args must drop the third argument
- `go vet ./...` and `go build ./...` are the quality gates (no test suite)
