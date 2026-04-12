# Implementation Plan: MCP Integration Test Suite

**Branch**: This project uses trunk-based development. Do not create feature branches.  
**Input**: Feature specification from `/specs/003-integration-test-mcp/spec.md`

## Summary

Add `integration_test.go` at the repository root that exercises all 13 operations across the `pets` and `bookstore` API specs via the `Registry` methods (`ListServices`, `ListOperations`, `GetOperationDetail`, `CallOperation`). An in-process `httptest.Server` captures all outbound HTTP requests; no real network calls are made. Table-driven subtests cover every operation, the write gate (confirmed=false blocks mutation), path-parameter substitution, and optional query-parameter handling.

## Technical Context

**Language/Version**: Go 1.23  
**Primary Dependencies**: `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3` (already in `go.mod`); `net/http/httptest` (stdlib — no new dependency)  
**Storage**: N/A — in-memory registry only  
**Testing**: `go test ./...` (stdlib `testing` + `net/http/httptest`)  
**Target Platform**: Linux / any platform that runs Go  
**Project Type**: Library / gateway  
**Performance Goals**: Each test case completes in < 1 s; full suite in < 5 s  
**Constraints**: No external network access; flat package layout (no new sub-packages); `go build ./...` and `go vet ./...` must pass  
**Scale/Scope**: 13 operations × 2 tools = ~26 primary test cases + ~8 write-gate blocking cases = ~34 table rows

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Code Quality | PASS | Single new file; `go build ./...` and `go vet ./...` enforced; no new exports needed |
| II. Testing Standards | PASS | This feature *is* the tests; table-driven; uses real in-process HTTP (httptest), no mocking of registry or HTTP client |
| III. Agent Interface Consistency | PASS | No changes to tool schemas or MCP tool handlers |
| IV. Performance Requirements | PASS | No production code changes; tests add no runtime overhead |
| Safety & Trust | PASS | No changes to write gate, header handling, or credential injection |
| Backward Compatibility & Versioning | PASS | No config schema changes; no public API changes |

## Project Structure

### Documentation (this feature)

```text
specs/003-integration-test-mcp/
├── plan.md                           # This file
├── spec.md                           # Feature specification
├── research.md                       # Phase 0 output
├── data-model.md                     # Phase 1 output
├── quickstart.md                     # Phase 1 output
├── contracts/
│   └── test-coverage-contract.md     # Phase 1 output
└── tasks.md                          # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
integration_test.go      # New — all integration tests (package portier)
apis/
├── pets.yaml            # Existing — used as test fixture
└── bookstore.yaml       # Existing — used as test fixture
```

All other files are unchanged.

**Structure Decision**: Single `integration_test.go` in the root package. The project mandates a flat layout; one new test file satisfies coverage without introducing sub-packages or new directories.

## Implementation Notes

### Stub server pattern

```go
// Captured state, reset per test case
type captured struct {
    method      string
    path        string
    rawQuery    string
    body        []byte
    contentType string
    called      bool
}

var last captured
stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    last = captured{
        method:      r.Method,
        path:        r.URL.Path,
        rawQuery:    r.URL.RawQuery,
        body:        body,
        contentType: r.Header.Get("Content-Type"),
        called:      true,
    }
    w.Header().Set("Content-Type", "application/json")
    fmt.Fprint(w, "{}")
}))
defer stub.Close()
```

### Registry setup

```go
reg := NewRegistry(stub.Client()) // stub.Client() routes to stub
falseVal := false
reg.LoadSpec(ServiceConfig{
    Name:                "pets",
    SpecPath:            "apis/pets.yaml",
    Host:                stub.URL,
    RequireConfirmation: &trueVal, // write gate active
})
```

### Table row example

```go
{
    name:        "listPets with limit",
    service:     "pets",
    operationID: "listPets",
    params:      map[string]any{"limit": 5},
    confirmed:   false,
    wantMethod:  "GET",
    wantPath:    "/pets",
    wantQuery:   "limit=5",
    wantBody:    nil,
    wantBlocked: false,
},
{
    name:        "createPet blocked without confirmation",
    service:     "pets",
    operationID: "createPet",
    params:      map[string]any{"name": "Fido"},
    confirmed:   false,
    wantBlocked: true,
},
{
    name:        "createPet confirmed",
    service:     "pets",
    operationID: "createPet",
    params:      map[string]any{"name": "Fido"},
    confirmed:   true,
    wantMethod:  "POST",
    wantPath:    "/pets",
    wantBody:    map[string]any{"name": "Fido"},
    wantBlocked: false,
},
```

### Known limitation: PATCH Content-Type

`CallOperation` always sets `Content-Type: application/json`, even for `patchBook` which the spec declares as `application/json-patch+json`. The test asserts `application/json` (current behaviour). A follow-up issue should track correcting the content-type header to match the spec's declared media type.

## Complexity Tracking

No constitution violations. No entries required.
