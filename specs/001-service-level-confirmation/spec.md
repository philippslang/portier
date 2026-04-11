# Feature Specification: Service-Level Confirmation Config

**Feature Branch**: `001-service-level-confirmation`  
**Created**: 2026-04-11  
**Status**: Draft  
**Input**: User description: "Currently the require confirmation is a configuration at the server level. Move it to the service level so it can be set per service."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Disable Confirmation for a Trusted Service (Priority: P1)

A gateway operator has two services: an internal write-heavy automation API that agents should call without friction, and a customer-facing billing service where mutating operations must always be confirmed before execution. The operator configures each service independently so that the automation API executes mutations without a confirmation prompt while the billing service still requires explicit confirmation.

**Why this priority**: This is the core ask. The feature delivers no value unless the confirmation flag can be set differently for at least two services in the same config.

**Independent Test**: Configure two services — one with confirmation disabled, one with confirmation enabled — start the gateway, call a mutating operation on each service, and verify only the confirmation-disabled service executes immediately while the other returns a confirmation prompt.

**Acceptance Scenarios**:

1. **Given** a config with service A (`require_confirmation: false`) and service B (`require_confirmation: true`), **When** an agent calls a POST operation on service A without `confirmed=true`, **Then** the operation executes and returns the API response.
2. **Given** the same config, **When** an agent calls a POST operation on service B without `confirmed=true`, **Then** the gateway returns a confirmation prompt and does not execute the call.
3. **Given** the same config, **When** an agent calls a POST operation on service B with `confirmed=true`, **Then** the operation executes and returns the API response.

---

### User Story 2 - Backward-Compatible Default Behavior (Priority: P2)

An existing operator upgrades to the new version without changing their config. Their current config only has `require_confirmation` at the server level (or omits it entirely). The gateway behaves exactly as before — confirmation required for all services — without any config migration.

**Why this priority**: Breaking existing deployments is unacceptable. The default must preserve the current behavior so operators who don't need per-service control see no change.

**Independent Test**: Start the gateway with a config that has no per-service `require_confirmation` field and verify that mutating operations still require confirmation by default.

**Acceptance Scenarios**:

1. **Given** a config with no `require_confirmation` field on any service, **When** an agent calls a POST operation without `confirmed=true`, **Then** the gateway returns a confirmation prompt (default-on behavior preserved).
2. **Given** a config where only the server-level `require_confirmation: false` is set and no per-service override exists, **When** an agent calls a POST operation without `confirmed=true`, **Then** the operation executes immediately (server-level setting still acts as the default for all services).

---

### User Story 3 - Per-Service Override of Server Default (Priority: P3)

An operator sets `require_confirmation: false` at the server level to disable confirmation globally, but wants one particularly sensitive service to still require confirmation. The operator adds `require_confirmation: true` to just that service's config entry to opt it back in.

**Why this priority**: Enables a "disable globally, opt specific services in" pattern, complementing Story 1's "enable globally, opt specific services out" pattern.

**Independent Test**: Set `require_confirmation: false` at the server level and `require_confirmation: true` on one service, then verify only that service still requires confirmation.

**Acceptance Scenarios**:

1. **Given** server-level `require_confirmation: false` and one service with `require_confirmation: true`, **When** an agent calls a mutating operation on the overriding service without `confirmed=true`, **Then** a confirmation prompt is returned.
2. **Given** the same config, **When** an agent calls a mutating operation on any other service without `confirmed=true`, **Then** the operation executes immediately.

---

### Edge Cases

- What happens when a service omits `require_confirmation` entirely — does it inherit the server default or fall back to the hardcoded default of `true`?
- What happens when both the service and server-level settings are omitted — is the effective value `true`?
- How does the `confirmed` parameter appear in the `call_operation` tool schema when services have mixed confirmation requirements (some require it, some do not)?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The service configuration MUST accept an optional `require_confirmation` field that controls the write gate for that service's mutating operations independently of other services.
- **FR-002**: When a service's `require_confirmation` is not set, the service MUST inherit the server-level `require_confirmation` value.
- **FR-003**: When neither the service nor the server specifies `require_confirmation`, the effective default MUST be `true` (confirmation required), preserving the current behavior.
- **FR-004**: The write gate check MUST be evaluated per-service at call time using each service's effective `require_confirmation` value.
- **FR-005**: The `confirmed` parameter MUST remain available in the `call_operation` tool interface so agents can confirm mutating calls on services that require it.
- **FR-006**: Existing configs that only use the server-level `require_confirmation` setting MUST continue to work without modification and without any change in observable behavior.

### Key Entities

- **ServiceConfig**: Extended with an optional `require_confirmation` field (tri-state: explicitly true / explicitly false / unset, meaning inherit from server).
- **ServerConfig**: Retains its `require_confirmation` field, which now acts as the gateway-wide default for services that do not specify their own value.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can configure two services with different confirmation requirements in a single config file, and each service enforces its own setting on every mutating call — verified without modifying the server-level default.
- **SC-002**: Existing config files with no per-service `require_confirmation` field produce identical observable behavior after the change, requiring zero operator action to migrate.
- **SC-003**: A service with `require_confirmation: false` never returns a confirmation prompt for mutating operations, regardless of whether the agent passes `confirmed=true` or omits it.
- **SC-004**: A service with an effective `require_confirmation: true` always returns a confirmation prompt for mutating operations when `confirmed` is absent or `false`, and executes the call when `confirmed=true`.

## Assumptions

- The server-level `require_confirmation` field is kept as the fallback default rather than removed, to avoid breaking existing operator configs.
- The `call_operation` tool is registered once for all services; per-service write-gate logic is applied at call dispatch time based on the target service's effective setting, not at tool registration time.
- The `confirmed` parameter in the `call_operation` tool interface remains a single boolean shared across all services; it is the write-gate enforcement that varies per service, not the parameter itself.
- Programmatic callers who embed portier tools in their own MCP server are expected to supply per-service confirmation state through the service registry rather than a single gateway-wide flag.
