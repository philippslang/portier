# Research: Replace method Field with confirmationRequired Boolean

**Feature**: 002-mutating-field (refined)
**Date**: 2026-04-11

## Decision 1: Field name — mutating vs confirmationRequired

**Decision**: Use `confirmationRequired` (camelCase, matching the existing field naming
convention in tool output: `operationId`, `requestBody`, `responseSchema`).

**Rationale**: The field name should match the codebase's JSON naming convention. More
importantly, the user's refinement makes the name even more precise — this field answers
"does this specific operation, on this specific service, require the agent to pass
confirmed=true?" That is a richer signal than `mutating` (which only encodes the HTTP
method semantics, ignoring service configuration).

**Alternatives considered**:
- `mutating`: only reflects the HTTP method, ignores `require_confirmation` config —
  rejected per user refinement request.
- `confirmation_required` (snake_case): inconsistent with the rest of the tool output
  field names.
- `requiresConfirmation`: reads more naturally in English but breaks camelCase
  consistency with `confirmationRequired`.

---

## Decision 2: Value computation

**Decision**: `confirmationRequired = svc.RequireConfirmation && isMutating(op.Method)`

This is exactly the same predicate used by the write gate in `CallOperation`:

```go
if svc.RequireConfirmation && isMutating(op.Method) && !confirmed {
    // return confirmation prompt
}
```

**Rationale**: The field MUST reflect the actual runtime behavior of the write gate.
An agent reading `confirmationRequired: false` on a service with `require_confirmation: false`
should never be surprised by a confirmation prompt — and it won't be, because the write gate
uses the identical predicate.

**Consequence**: For a service with `require_confirmation: false`, ALL operations
(including POST/PUT/PATCH/DELETE) will show `confirmationRequired: false`. This is
correct and desirable — the agent should not prepare a `confirmed=true` call for a
service that has the gate disabled.

**Alternatives considered**:
- `isMutating(op.Method)` alone: ignores service config — leads to false signals when
  `require_confirmation: false` on a service.
- `svc.RequireConfirmation` alone: ignores the HTTP method — would mark GET operations
  as `confirmationRequired: true` on services with the gate enabled, which is wrong.

---

## Decision 3: Where to compute — ListOperations and GetOperationDetail

**Decision**: Compute inline in both `ListOperations` and `GetOperationDetail`.
Both methods already have `svc` in scope (the `Service` struct), which carries the
resolved `RequireConfirmation bool` field added in feature 001. No new parameters needed.

```go
// In ListOperations
"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),

// In GetOperationDetail
"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
```

**Rationale**: Both call sites already hold a reference to `svc` — no architectural change
required. The `Service.RequireConfirmation` field was specifically designed to carry the
resolved effective value, so this is exactly the right place to use it.

**Alternatives considered**:
- Add `ConfirmationRequired bool` to the `Operation` struct and populate in `LoadSpec`:
  unnecessary — the value can change if `require_confirmation` is updated; computing
  it inline keeps the logic co-located with the write gate predicate.

---

## Decision 4: Remove method from tool output?

**Decision**: Yes — remove `method` from both `list_operations` and `get_operation_detail`
output. The original motivation (replacing `method` with a more semantic field) stands.
`confirmationRequired` subsumes the write-gate signal `method` was providing. The HTTP
verb remains on the `Operation` struct for internal use by `CallOperation`.

**Rationale**: `confirmationRequired` gives the agent everything it needs to decide
whether to pass `confirmed=true`. Keeping `method` alongside it would be redundant for
that purpose and would re-expose an implementation detail.

**Alternatives considered**:
- Keep `method` and add `confirmationRequired`: redundant; `method` reverts to being
  noise rather than signal.

---

## Summary of changes required

| File | Change |
|------|--------|
| `registry.go` | `ListOperations`: replace `"method": op.Method` with `"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method)` |
| `registry.go` | `GetOperationDetail`: replace `"method": op.Method` with `"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method)` |
| `tools.go` | Update `list_operations` description to mention `confirmationRequired` |
| `tools.go` | Update `get_operation_detail` description to mention `confirmationRequired` |
