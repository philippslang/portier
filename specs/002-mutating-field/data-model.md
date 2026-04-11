# Data Model: Replace method Field with confirmationRequired Boolean

**Feature**: 002-mutating-field (refined)
**Date**: 2026-04-11

## Runtime Structs — unchanged

The `Operation` and `Service` structs in `registry.go` are **not changed**.
`Method string` stays on `Operation` for internal use by `CallOperation`.
`RequireConfirmation bool` on `Service` (added in feature 001) is read here but not modified.

```
Operation
├── OperationID  string                    (unchanged — internal + output)
├── Summary      string                    (unchanged — internal + output)
├── Method       string                    (unchanged — internal only, NOT returned to agents)
├── Path         string                    (unchanged — internal + output)
├── Tags         []string                  (unchanged — internal + output)
├── Parameters   openapi3.Parameters       (unchanged)
├── RequestBody  *openapi3.RequestBodyRef   (unchanged)
└── Responses    *openapi3.Responses        (unchanged)

Service
└── RequireConfirmation bool   (from feature 001 — read here, not modified)
```

## Tool Output Schema — changed fields

### list_operations response (per operation)

```
Before:
  operationId           string
  summary               string
  method                string   ← removed
  tags                  []string

After:
  operationId           string
  summary               string
  confirmationRequired  bool     ← new: svc.RequireConfirmation && isMutating(op.Method)
  tags                  []string
```

### get_operation_detail response

```
Before:
  operationId           string
  method                string   ← removed
  path                  string
  summary               string
  parameters            [...]
  requestBody           {...}    (optional)
  responseSchema        {...}    (optional)

After:
  operationId           string
  confirmationRequired  bool     ← new: svc.RequireConfirmation && isMutating(op.Method)
  path                  string
  summary               string
  parameters            [...]
  requestBody           {...}    (optional)
  responseSchema        {...}    (optional)
```

## confirmationRequired field semantics

```
confirmationRequired = svc.RequireConfirmation && isMutating(op.Method)
```

This is identical to the write gate predicate in `CallOperation`:

```go
if svc.RequireConfirmation && isMutating(op.Method) && !confirmed { ... }
```

The agent can trust this field completely: `confirmationRequired: true` means the call
will be blocked without `confirmed=true`; `confirmationRequired: false` means the call
executes immediately regardless of `confirmed`.

### Example values across configurations

| Service require_confirmation | HTTP method | confirmationRequired |
|------------------------------|-------------|----------------------|
| true (default)               | POST        | true                 |
| true (default)               | GET         | false                |
| false                        | POST        | false                |
| false                        | GET         | false                |

## Breaking change assessment

**Breaking** — agents relying on `method` in `list_operations` or `get_operation_detail`
responses will stop receiving that field. The new `confirmationRequired` field is a
direct, unambiguous replacement for the write-gate signal that `method` was previously
used to infer.
