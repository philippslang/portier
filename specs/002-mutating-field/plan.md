# Implementation Plan: Replace method Field with confirmationRequired Boolean

**Branch**: This project uses trunk-based development. Do not create feature branches.
**Input**: "Replace the method field in the tool schema by an explicit mutating: true/false field."
**Refined**: "Instead of mutating: bool, use confirmationRequired: bool. This respects the service and server-level configuration require_confirmation."

## Summary

Remove the `method` string field from the `list_operations` and `get_operation_detail`
tool responses. Replace it with `confirmationRequired` (bool), computed as
`svc.RequireConfirmation && isMutating(op.Method)` — the same predicate used by the
write gate in `CallOperation`. This is more informative than a raw `mutating` flag
because it reflects both the HTTP method AND the service's effective `require_confirmation`
setting. The `Method` field stays on the internal `Operation` struct. Both output sites
(`ListOperations`, `GetOperationDetail`) already have `svc` in scope.

## Technical Context

**Language/Version**: Go 1.23 (module `github.com/philippslang/portier`)
**Primary Dependencies**: `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3`
**Storage**: N/A — in-memory registry
**Testing**: No test suite currently (`go build ./...` + `go vet ./...` are quality gates)
**Target Platform**: Linux server / any Go-supported platform
**Project Type**: Library + CLI
**Performance Goals**: No change — one boolean AND per operation in the output path
**Constraints**: Breaking change to agent-facing tool output (justified — see Constitution Check)
**Scale/Scope**: 2 functions in `registry.go`, 2 description strings in `tools.go` — ~10 lines total
**Dependency**: Requires `Service.RequireConfirmation bool` from feature 001 (already merged)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Code Quality | PASS | `go build` + `go vet` pass; no new exports; no new abstraction; `isMutating` reused |
| II. Testing Standards | PASS | No test suite; output field substitution is verifiable by inspection and build |
| III. Agent Interface Consistency | JUSTIFIED BREAK | `method` → `confirmationRequired` is breaking; justified: the new field is the authoritative write-gate signal, not an implementation detail; agents gain a direct, config-aware answer |
| IV. Performance Requirements | PASS | One boolean AND per operation; negligible; no depth/truncation change |
| Safety & Trust | PASS | No credential or header exposure affected; write gate predicate unchanged |
| Backward Compatibility & Versioning | JUSTIFIED BREAK | Same as III; minor version bump appropriate (new capability, old field removed) |

## Project Structure

### Documentation (this feature)

```text
specs/002-mutating-field/
├── plan.md              ← this file
├── research.md          ← Phase 0 complete
├── data-model.md        ← Phase 1 complete
├── contracts/
│   └── tool-schemas.md  ← Phase 1 complete
└── tasks.md             ← /speckit.tasks — not yet created
```

### Source Code (repository root)

```text
registry.go   ← ListOperations + GetOperationDetail: "method" → "confirmationRequired"
tools.go      ← list_operations + get_operation_detail descriptions updated
```

No new files. No new directories. No struct changes.

## Implementation Guide

### registry.go — ListOperations

```go
// Before
result = append(result, map[string]any{
    "operationId": op.OperationID,
    "summary":     op.Summary,
    "method":      op.Method,
    "tags":        op.Tags,
})

// After
result = append(result, map[string]any{
    "operationId":          op.OperationID,
    "summary":              op.Summary,
    "confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
    "tags":                 op.Tags,
})
```

### registry.go — GetOperationDetail

```go
// Before
detail := map[string]any{
    "operationId": op.OperationID,
    "method":      op.Method,
    "path":        op.Path,
    "summary":     op.Summary,
}

// After
detail := map[string]any{
    "operationId":          op.OperationID,
    "confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
    "path":                 op.Path,
    "summary":              op.Summary,
}
```

### tools.go — list_operations description

```go
mcp.WithDescription("List operations available in a service. Returns operationId, summary, confirmationRequired flag (true when the operation requires confirmed=true to execute), and tags. Optionally filter by tag."),
```

### tools.go — get_operation_detail description

```go
mcp.WithDescription("Get full parameter schemas, request body schema, and response schema for an operation. Also returns confirmationRequired indicating whether you must pass confirmed=true when calling this operation. Use this to understand how to call an operation before calling it."),
```

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| Breaking agent contract (III) | `method` exposes HTTP implementation detail; `confirmationRequired` gives the exact runtime signal the agent needs | Keeping both fields adds noise; `confirmationRequired` subsumes all information `method` was used to infer |
