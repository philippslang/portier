# Implementation Plan: Service-Level Confirmation Config

**Branch**: `001-service-level-confirmation` | **Date**: 2026-04-11 | **Spec**: [spec.md](spec.md)  
**Input**: Feature specification from `/specs/001-service-level-confirmation/spec.md`

## Summary

Move `require_confirmation` from a server-wide flag to an optional per-service override that inherits from the server default when not set. Four files change: `config.go` adds `*bool` to `ServiceConfig`, `registry.go` adds `bool` to `Service` and updates the write gate predicate, `tools.go` removes the `writeGate` parameter from `RegisterTools`, and `server.go` resolves per-service inheritance before calling `LoadSpec`.

## Technical Context

**Language/Version**: Go 1.23 (module `github.com/philippslang/portier`)  
**Primary Dependencies**: `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3`, `go.opentelemetry.io/otel`  
**Storage**: N/A — in-memory registry only  
**Testing**: No test suite currently  
**Target Platform**: Linux server / any Go-supported platform  
**Project Type**: Library + CLI  
**Performance Goals**: No change — one additional boolean AND in the hot path is negligible  
**Constraints**: Fully backward-compatible config; breaking change to `RegisterTools` public Go API (third argument removed)  
**Scale/Scope**: 4 files, ~30 lines changed

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Code Quality | PASS | `go build` + `go vet` pass; all exports documented; no new sub-packages |
| II. Testing Standards | PASS | No test suite exists yet; no behavioral regression introduced; write gate logic is simple enough for manual verification |
| III. Agent Interface Consistency | PASS | `call_operation` tool schema unchanged except `confirmed` is now always present (previously conditional) — additive, not breaking |
| IV. Performance Requirements | PASS | One boolean AND added to the hot path; negligible overhead; truncation/depth limits unchanged |
| Safety & Trust | PASS | No header exposure change; write gate default preserved (`true` when unset) |
| Backward Compatibility & Versioning | PASS | `ServiceConfig.RequireConfirmation *bool` uses pointer type for nil-safe inheritance; existing configs require no changes |

## Project Structure

### Documentation (this feature)

```text
specs/001-service-level-confirmation/
├── plan.md              ← this file
├── research.md          ← Phase 0 complete
├── data-model.md        ← Phase 1 complete
├── quickstart.md        ← Phase 1 complete
├── contracts/
│   └── config-schema.md ← Phase 1 complete
└── tasks.md             ← Phase 2 (/speckit.tasks — not yet created)
```

### Source Code (repository root)

```text
config.go      ← add RequireConfirmation *bool to ServiceConfig
registry.go    ← add RequireConfirmation bool to Service; update LoadSpec and CallOperation
tools.go       ← remove writeGate bool from RegisterTools; always include confirmed param
server.go      ← resolve per-service inheritance; update RegisterTools call
config.yaml    ← document new optional field (comment)
```

**Structure Decision**: Single flat package — no new files, no new directories. All changes are confined to the four existing source files.

## Implementation Guide

### 1. config.go — add `*bool` to ServiceConfig

Add after `Headers`:

```go
// RequireConfirmation controls the write gate for this service's mutating operations.
// When nil, the server-level require_confirmation setting is used.
// When true, POST/PUT/PATCH/DELETE require confirmed=true.
// When false, mutating operations execute immediately without confirmation.
RequireConfirmation *bool `yaml:"require_confirmation,omitempty"`
```

No changes to `LoadConfig` — the server default (`true`) is already set there. Service-level nil fields are resolved in `NewServer`.

### 2. registry.go — Service struct + LoadSpec + CallOperation

**Service struct** — add resolved field:

```go
RequireConfirmation bool  // resolved at LoadSpec time
```

**LoadSpec** — resolve from config, nil defaults to `true`:

```go
requireConfirmation := true
if cfg.RequireConfirmation != nil {
    requireConfirmation = *cfg.RequireConfirmation
}
svc := &Service{
    // ... existing fields ...
    RequireConfirmation: requireConfirmation,
}
```

**CallOperation** — update write gate predicate:

```go
// Old
if isMutating(op.Method) && !confirmed {

// New
if svc.RequireConfirmation && isMutating(op.Method) && !confirmed {
```

### 3. tools.go — remove writeGate parameter

**Signature change**:

```go
// Old
func RegisterTools(s *mcpserver.MCPServer, reg *Registry, writeGate bool) {

// New
func RegisterTools(s *mcpserver.MCPServer, reg *Registry) {
```

**Tool description** — always use the confirmation-aware description:

```go
mcp.WithDescription("Execute an API operation. Pass all path, query, and body parameters as a flat object in 'params'. Mutating operations (POST/PUT/PATCH/DELETE) on services that require confirmation will return a confirmation prompt unless confirmed=true."),
```

**confirmed parameter** — always add it:

```go
callToolOpts = append(callToolOpts,
    mcp.WithBoolean("confirmed",
        mcp.Description("Set to true to confirm a mutating (POST/PUT/PATCH/DELETE) operation on services that require confirmation"),
    ),
)
```

**Handler** — always read `confirmed` from args:

```go
confirmed, _ := args["confirmed"].(bool)
```

Remove the `writeGate` conditional block entirely.

### 4. server.go — resolve inheritance + update call

**Before `reg.LoadSpec(svcCfg)`**, fill nil from server default:

```go
for _, svcCfg := range cfg.Services {
    if svcCfg.RequireConfirmation == nil {
        v := cfg.Server.RequireConfirmation
        svcCfg.RequireConfirmation = &v
    }
    if err := reg.LoadSpec(svcCfg); err != nil {
        // existing error handling unchanged
    }
}
```

**RegisterTools call** — remove third argument:

```go
// Old
RegisterTools(s, reg, cfg.Server.RequireConfirmation)

// New
RegisterTools(s, reg)
```

## Complexity Tracking

No constitution violations. No complexity to justify.
