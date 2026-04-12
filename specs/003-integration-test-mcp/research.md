# Research: MCP Integration Test Suite

## Decision 1: Test entry point — registry methods vs MCP protocol layer

**Decision**: Test through `Registry` methods (`ListServices`, `ListOperations`, `GetOperationDetail`, `CallOperation`) directly, not through the JSON-RPC MCP protocol.

**Rationale**: The MCP tool handlers in `tools.go` are thin wrappers that parse arguments from a `map[string]any` and delegate to the registry. All meaningful behaviour — URL construction, write gate, body serialisation, header injection — lives in `registry.go`. Testing at the registry boundary exercises the real logic with no indirection, while testing at the JSON-RPC layer would add protocol boilerplate without covering additional logic paths. The constitution explicitly prohibits mocking the registry or HTTP client where an integration approach is feasible; the registry-direct approach satisfies "integration" since all real code paths execute.

**Alternatives considered**:
- Full MCP JSON-RPC protocol: More faithful to external callers but requires running an MCP server, encoding/decoding JSON-RPC, and handling protocol state — overhead that dwarfs the actual logic under test.
- Testing only `CallOperation`: Would miss coverage of `ListOperations` tag filtering and `GetOperationDetail` schema flattening that the spec requires.

---

## Decision 2: HTTP stub strategy

**Decision**: Use `net/http/httptest.NewServer` with a handler that records all inbound requests into a slice. The stub always responds 200 with `{}` or `[]` depending on the request.

**Rationale**: `httptest.Server` is in-process, zero-dependency, and integrates naturally with `http.Client`. A registry is created with a custom `*http.Client` whose base URL points at the stub. `LoadSpec` accepts a `ServiceConfig.Host` override that replaces the spec's server URL, so the stub intercepts all outbound calls without modifying any production code.

**Alternatives considered**:
- External HTTP mock library (`httpmock`, `gock`): Adds a dependency and wraps `http.DefaultTransport`, which may interfere with tests running in parallel or introduce global state.
- Intercepting at `http.RoundTripper`: Valid, but `httptest.Server` achieves the same with less boilerplate.

---

## Decision 3: Test file location and package

**Decision**: Single file `integration_test.go` in the root package (`package portier`), using `_test.go` build tag so it is compiled only during `go test`.

**Rationale**: The project's constitution mandates a flat package layout. A `_test.go` file in the root package gives access to unexported helpers if needed while staying flat. Keeping it in the same package as the production code aligns with idiomatic Go integration tests for library packages.

**Alternatives considered**:
- Separate `tests/` directory with `package portier_test`: Requires all types to be exported. Acceptable but adding a sub-package for test code is unnecessary given the flat-layout rule.
- `package portier_test` in root: A black-box test package in the same directory; cleaner boundary but loses access to internals. Acceptable and slightly preferred from an encapsulation standpoint — chosen as secondary option if internal access proves unnecessary (it won't be needed).

---

## Decision 4: Table-driven test structure

**Decision**: One table-driven test function per tool (`TestListOperations`, `TestGetOperationDetail`, `TestCallOperation`), each with named subtests using `t.Run(tc.name, ...)`.

**Rationale**: The constitution requires table-driven tests where multiple input/output combinations exist. Each operation in the two API specs is a row. Subtests allow `go test -run TestCallOperation/createPet` for targeted debugging.

**Alternatives considered**:
- One test per operation: 13 operations × 3 tools = 39 functions, very repetitive.
- Single flat loop with no subtests: Harder to identify which operation failed.

---

## Decision 5: Content-Type for PATCH (RFC 6902)

**Decision**: The current `CallOperation` implementation always sets `Content-Type: application/json` regardless of the spec's declared content type. The `patchBook` operation declares `application/json-patch+json`. The test will assert `application/json` (matching current behaviour), and a note will be added to `research.md` flagging that content-type fidelity is a known gap.

**Rationale**: Fixing the content-type discrepancy is out of scope for this feature (it is a separate behaviour change). The integration test documents current behaviour accurately; if the content-type is later corrected, the test will need updating — which is the right signal.

**Alternatives considered**:
- Assert `application/json-patch+json`: Would cause the test to fail immediately and block this feature on an unrelated fix.
- Skip the PATCH content-type assertion: Partial coverage; still assert method and URL.

---

## Decision 6: Stub response shape

**Decision**: Stub returns `{}` (empty JSON object) for all requests. The status is always 200.

**Rationale**: `CallOperation` calls `json.Unmarshal` on the body and wraps it in `{"status": 200, "data": ...}`. An empty object satisfies the unmarshal step without simulating any domain logic. The test does not assert the returned `data` — only the outbound HTTP request captured by the stub.

**Alternatives considered**:
- Return `[]` for list endpoints: More realistic but unnecessary; the truncation logic is tested via the outbound request, not the response body.
- Return schema-conformant bodies: Would require per-operation stub responses, adding maintenance burden for no additional test coverage of the gateway.
