# Contract: Integration Test Coverage

This document defines the minimum coverage contract that the integration test suite must satisfy. It is technology-agnostic and serves as the verifiable acceptance gate.

---

## Coverage Invariants

### CI-001: Every operation is covered

For every `operationId` defined in `apis/pets.yaml` and `apis/bookstore.yaml`, there MUST exist at least one test case in `TestCallOperation` that:
- Passes `confirmed=true` and asserts the expected HTTP method and URL path.

**Verification**: Run `go test -v -run TestCallOperation ./...` and confirm every operation name appears as a subtest.

---

### CI-002: Write gate is tested for every mutating operation

For every mutating operation (POST, PUT, PATCH, DELETE), there MUST exist an additional test case with `confirmed=false` that:
- Asserts no HTTP request reached the stub.
- Asserts the result contains `"status": "confirmation_required"`.

**Verification**: Grep for `wantBlocked: true` in `integration_test.go`; count must equal the number of mutating operations (8 across both specs).

---

### CI-003: Path parameter substitution is verified

For every operation with a `{param}` in its path, the test case MUST supply a non-trivial value (not empty string) and assert the substituted value appears in the captured URL path.

**Verification**: All operations with path params appear in the table with a concrete ID value (e.g. `"petId": "pet-42"`).

---

### CI-004: Optional query parameters are tested

For at least one GET operation per service that has an optional query parameter (`listPets?limit`, `listBooks?limit`, `listBooks?genre`), there MUST be:
1. A case that supplies the parameter and asserts it appears in the query string.
2. A case that omits the parameter and asserts the query string is empty.

---

### CI-005: No external network access

The test suite MUST NOT make any real outbound HTTP calls. All requests are intercepted by the in-process `httptest.Server`.

**Verification**: The registry used in tests is constructed with a custom `*http.Client` whose transport resolves to the stub URL only; the stub URL is `127.0.0.1:<ephemeral-port>`.

---

### CI-006: Discovery tools return correct metadata

`TestListOperations` MUST verify:
- Tag filtering: calling with tag `"books"` returns only book operations, not review operations.
- No filter: calling without a tag returns all operations for the service.

`TestGetOperationDetail` MUST verify:
- A required field appears in `parameters` with `required: true`.
- A response schema is present for operations with a 200/201 response.

---

## Stub Response Contract

The stub server responds to all inbound requests with:

```
HTTP/1.1 200 OK
Content-Type: application/json

{}
```

This is the minimum valid response that prevents `CallOperation` from returning a JSON parse error.
