# Data Model: Service-Level Confirmation Config

**Feature**: 001-service-level-confirmation  
**Date**: 2026-04-11

## Config Structs (config.go)

### ServiceConfig — changed field

```
ServiceConfig
├── Name              string           (unchanged)
├── SpecPath          string           (unchanged)
├── Host              string           (unchanged)
├── BasePath          string           (unchanged)
├── AllowOperations   []string         (unchanged)
├── IgnoreHeaders     []string         (unchanged)
├── Headers           map[string]string (unchanged)
└── RequireConfirmation *bool          NEW — nil = inherit server default
                                            true  = always require confirmation
                                            false = never require confirmation
```

YAML tag: `require_confirmation` (omitempty). Absent in YAML → `nil` after unmarshal.

### ServerConfig — unchanged

`RequireConfirmation bool` remains. It is the fallback default for any service whose `RequireConfirmation` is `nil`. Its default value in `LoadConfig` stays `true`.

## Runtime Structs (registry.go)

### Service — new field

```
Service
├── Name              string           (unchanged)
├── Description       string           (unchanged)
├── BaseURL           string           (unchanged)
├── Operations        map[string]*Operation (unchanged)
├── Tags              []string         (unchanged)
├── StaticHeaders     map[string]string (unchanged)
└── RequireConfirmation bool           NEW — resolved effective value
                                            true  = write gate active for this service
                                            false = write gate disabled for this service
```

The resolved value is computed once at `LoadSpec` time and stored here. No nil pointers in the runtime path.

## Resolution Logic

```
Effective RequireConfirmation =
  if ServiceConfig.RequireConfirmation != nil:
    *ServiceConfig.RequireConfirmation
  else:
    true   (hardcoded default — safe for programmatic LoadSpec callers)
```

The server-level default is injected by `NewServer` *before* calling `LoadSpec`:

```
if svcCfg.RequireConfirmation == nil:
    svcCfg.RequireConfirmation = &cfg.Server.RequireConfirmation
```

This means:
- Config-driven callers (`NewServer`): service inherits server default if not set.
- Programmatic callers (`LoadSpec` directly): service defaults to `true` if not set.

## Write Gate Predicate (registry.go — CallOperation)

Old: `isMutating(op.Method) && !confirmed`  
New: `svc.RequireConfirmation && isMutating(op.Method) && !confirmed`

No other changes to `CallOperation` logic.

## Tool Schema (tools.go — RegisterTools)

Old signature: `RegisterTools(s *MCPServer, reg *Registry, writeGate bool)`  
New signature: `RegisterTools(s *MCPServer, reg *Registry)`

The `confirmed` boolean parameter is **always** included in the `call_operation` tool schema, regardless of individual service settings. The tool description is updated to reflect that confirmation is service-dependent.

## Validation Rules

No new validation rules. The existing `LoadConfig` validations are unchanged. An absent `require_confirmation` field is valid (nil pointer, resolved at runtime).

## Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| Config with only server-level `require_confirmation: true` (current default) | All services resolve to `true` — identical to current behavior |
| Config with only server-level `require_confirmation: false` | All services resolve to `false` — identical to current behavior |
| Config with no `require_confirmation` anywhere | Server default is `true`; all services inherit → `true` — identical to current behavior |
| Config with per-service `require_confirmation: false`, server default `true` | Only that service bypasses the write gate |
| Programmatic `LoadSpec` with `RequireConfirmation: nil` | Resolves to `true` — safe default |
