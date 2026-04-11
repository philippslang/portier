# Contract: MCP Tool Output Schemas

**Feature**: 002-mutating-field (refined)
**Date**: 2026-04-11

## list_operations — response item (updated)

Each item in the returned array:

```json
{
  "operationId":          "createPet",
  "summary":              "Create a pet",
  "confirmationRequired": true,
  "tags":                 ["pets"]
}
```

```json
{
  "operationId":          "listPets",
  "summary":              "List all pets",
  "confirmationRequired": false,
  "tags":                 ["pets"]
}
```

Removed field: `"method"`
Added field: `"confirmationRequired"` — `true` iff the service's `require_confirmation`
is enabled AND the operation's HTTP method is POST, PUT, PATCH, or DELETE.

## get_operation_detail — response (updated)

```json
{
  "operationId":          "createPet",
  "confirmationRequired": true,
  "path":                 "/pets",
  "summary":              "Create a pet",
  "parameters":           [],
  "requestBody":          { "...": "..." }
}
```

Removed field: `"method"`
Added field: `"confirmationRequired"`

## call_operation — unchanged

The `call_operation` tool interface is unchanged. The agent passes `service`,
`operationId`, `params`, and optionally `confirmed`. The HTTP method is resolved
internally. The `confirmed` parameter remains in the schema for services that require it.

## Behavioral guarantee

`confirmationRequired` in the tool output and the write gate in `call_operation` use
the same predicate. An agent that passes `confirmed=true` when and only when
`confirmationRequired=true` will never receive an unexpected confirmation prompt and
will never accidentally skip a required confirmation.

## Migration note for agents

Before: agent checks `method === "POST" || method === "PUT" || ...`  
After:  agent checks `confirmationRequired === true`

The new field is authoritative — it incorporates both the HTTP method semantics and
the per-service `require_confirmation` configuration.
