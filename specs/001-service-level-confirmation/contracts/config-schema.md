# Contract: Configuration Schema

**Feature**: 001-service-level-confirmation  
**Date**: 2026-04-11

## config.yaml — service entry (updated)

```yaml
services:
  - name: <string>                     # required
    spec: <path>                       # required
    host: <string>                     # optional — overrides spec server host
    base_path: <string>                # optional — overrides spec base path
    allow_operations: [<string>, ...]  # optional — operation allow list
    ignore_headers: [<string>, ...]    # optional — headers to strip
    headers:                           # optional — static auth headers
      <Header-Name>: <value>
    require_confirmation: <bool>       # NEW optional — per-service write gate
                                       #   true  = require confirmed=true for mutations
                                       #   false = execute mutations without confirmation
                                       #   absent = inherit server-level setting
```

## Inheritance chain

```
absent service field
  → server.require_confirmation value
      → default: true
```

## config.yaml — server section (unchanged interface)

```yaml
server:
  require_confirmation: <bool>   # default: true — fallback for services that omit it
```

## call_operation tool — parameter contract (updated)

The `confirmed` boolean parameter is always present in the tool schema.

```json
{
  "name": "call_operation",
  "parameters": {
    "service":     { "type": "string",  "required": true },
    "operationId": { "type": "string",  "required": true },
    "params":      { "type": "object",  "required": false },
    "confirmed":   { "type": "boolean", "required": false }
  }
}
```

Behavior:
- For services with effective `require_confirmation: true`: mutations without `confirmed=true` return a confirmation prompt.
- For services with effective `require_confirmation: false`: mutations execute regardless of `confirmed`.

## Go public API — RegisterTools (updated signature)

```go
// Before (breaking change)
func RegisterTools(s *mcpserver.MCPServer, reg *Registry, writeGate bool)

// After
func RegisterTools(s *mcpserver.MCPServer, reg *Registry)
```

Callers must remove the third argument. No functional change for callers that previously passed `true` (the default behavior is preserved through service-level config). Callers that passed `false` must now set `require_confirmation: false` on each service in their `ServiceConfig`.
