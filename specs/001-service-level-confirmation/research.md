# Research: Service-Level Confirmation Config

**Feature**: 001-service-level-confirmation  
**Date**: 2026-04-11

## Decision 1: Representing optional boolean in Go config struct

**Decision**: Use `*bool` (pointer to bool) for `ServiceConfig.RequireConfirmation`.

**Rationale**: Go has no built-in nullable/optional primitive. A pointer distinguishes three states: `nil` (not set, inherit), `true` (explicitly enabled), `false` (explicitly disabled). This maps cleanly to YAML unmarshalling — an absent field produces `nil`, `true` produces a pointer to `true`, `false` produces a pointer to `false`. The existing `ServiceConfig` struct already uses `omitempty` pointer fields for optional string overrides (`Host`, `BasePath`), so this is consistent with the existing pattern.

**Alternatives considered**:
- A custom tri-state type (enum): more expressive but adds unnecessary complexity for a binary flag.
- A separate `RequireConfirmationSet bool` sentinel field: verbose and error-prone.
- Resolving at YAML-unmarshal time with a custom unmarshaler: adds complexity for no gain over `*bool`.

---

## Decision 2: Where to resolve the effective value (nil → concrete bool)

**Decision**: Resolve at `NewServer` before calling `reg.LoadSpec`, by filling `nil` service fields from the server-level default. `LoadSpec` treats a `nil` pointer as `true` (hardcoded default) as a safety net for programmatic usage without a server config.

**Rationale**: `NewServer` is the natural place where both the server config and service configs are in scope simultaneously. Pre-filling the `nil` service field there keeps `LoadSpec` simple and self-contained. Programmatic callers who use `LoadSpec` directly without a server config get the safe default of `true`. The resolution logic is concentrated in one place.

**Alternatives considered**:
- Passing `serverDefault bool` as a second argument to `LoadSpec`: changes the public API more invasively; programmatic callers must always supply the default even when it isn't needed.
- Resolving lazily in `CallOperation` by checking `*bool` at call time: scatters nil-checks across the hot path and requires the `Service` struct to hold `*bool` instead of `bool`, complicating every downstream caller.

---

## Decision 3: `RegisterTools` signature — remove `writeGate bool`

**Decision**: Remove the `writeGate bool` parameter from `RegisterTools`. The `confirmed` parameter is always included in the `call_operation` tool schema. The write gate is enforced per-service inside `CallOperation`.

**Rationale**: With per-service settings, a single `writeGate` flag no longer models reality — different services within the same gateway can have different settings. Always including the `confirmed` parameter is safe (it is ignored when the service's effective setting is `false`). The CLAUDE.md public-API example already documents `RegisterTools(myMCPServer, reg)` with two arguments, so this aligns the implementation with the documented API.

**Alternatives considered**:
- Keep `writeGate` as a global override that can suppress `confirmed` from the schema: adds a second source of truth alongside per-service settings, creating confusion.
- Dynamically register separate tools for services with vs. without a write gate: not possible — MCP tools are registered once and keyed by name; there is one `call_operation` tool.
- Compute `writeGate = any service has require_confirmation true` and pass that in: still requires the server to know about all services at registration time; simpler to just always include `confirmed`.

---

## Decision 4: Write gate check in `CallOperation`

**Decision**: Change the write gate predicate from `isMutating(op.Method) && !confirmed` to `svc.RequireConfirmation && isMutating(op.Method) && !confirmed`.

**Rationale**: `CallOperation` already holds a reference to the `svc` struct, which will carry the resolved `RequireConfirmation bool`. Adding one boolean AND to the existing check is minimal and localized. No new parameters are needed.

**Alternatives considered**:
- Checking in the tool handler (tools.go) before calling `CallOperation`: splits the write gate logic across two files and requires the handler to know about service-level config.
- Passing `requireConfirmation bool` as a parameter to `CallOperation`: redundant since the registry already owns the service data.

---

## Summary of changes required

| File | Change |
|------|--------|
| `config.go` | Add `RequireConfirmation *bool` to `ServiceConfig`; default resolution logic in `LoadConfig` is unchanged (server default stays `true`) |
| `registry.go` | Add `RequireConfirmation bool` to `Service`; resolve in `LoadSpec` from `cfg.RequireConfirmation` (nil → `true`); update write gate predicate in `CallOperation` |
| `tools.go` | Remove `writeGate bool` parameter from `RegisterTools`; always include `confirmed` parameter; always read `confirmed` from args |
| `server.go` | Pre-fill `nil` service `RequireConfirmation` from `cfg.Server.RequireConfirmation` before calling `reg.LoadSpec`; update `RegisterTools` call |
| `config.yaml` | Document the new per-service field (optional) |
