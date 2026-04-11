# Quickstart: Service-Level Confirmation Config

**Feature**: 001-service-level-confirmation  
**Date**: 2026-04-11

## Scenario: mixed confirmation requirements

```yaml
server:
  require_confirmation: true   # default — all services require confirmation unless overridden

services:
  - name: billing
    spec: specs/billing.yaml
    # require_confirmation not set → inherits server default (true)
    # mutations on billing always return a confirmation prompt

  - name: automation
    spec: specs/automation.yaml
    require_confirmation: false
    # mutations on automation execute immediately, no confirmation needed
```

## Scenario: disable globally, opt one service back in

```yaml
server:
  require_confirmation: false  # no confirmation globally

services:
  - name: public-api
    spec: specs/public.yaml
    # inherits false — no confirmation required

  - name: admin
    spec: specs/admin.yaml
    require_confirmation: true
    # overrides global default — admin mutations still require confirmation
```

## Programmatic usage (no server config)

```go
reg := portier.NewRegistry(nil)

// RequireConfirmation nil → defaults to true (safe default)
reg.LoadSpec(portier.ServiceConfig{
    Name:     "petstore",
    SpecPath: "petstore.yaml",
})

// Explicitly disable confirmation for this service
disabled := false
reg.LoadSpec(portier.ServiceConfig{
    Name:                 "internal",
    SpecPath:             "internal.yaml",
    RequireConfirmation:  &disabled,
})

portier.RegisterTools(myMCPServer, reg)  // no writeGate arg
```

## Migration from previous version

If you called `RegisterTools` with three arguments, remove the third:

```go
// Before
portier.RegisterTools(s, reg, cfg.Server.RequireConfirmation)

// After
portier.RegisterTools(s, reg)
```

The write gate behavior is now derived from each service's `RequireConfirmation` field, which is automatically populated from your config by `NewServer`.
