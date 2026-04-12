# Feature Specification: MCP Integration Test Suite

**Feature Branch**: `003-integration-test-mcp`  
**Created**: 2026-04-11  
**Status**: Draft  
**Input**: User description: "Add an integration test. Use the API definitions in specs and test that all operations triggered through the MCP tools result in the expected HTTP request."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Read Operations Produce Correct HTTP Requests (Priority: P1)

A developer adding or modifying the gateway needs confidence that every read (non-mutating) operation exposed through the `call_operation` MCP tool is translated into exactly the right HTTP request — correct method, URL, path parameters, query parameters, and headers — before those changes reach production.

**Why this priority**: Read operations are the most frequently used path and serve as the baseline contract for the gateway. Verifying them first catches the broadest class of regressions.

**Independent Test**: Can be verified by invoking `call_operation` for each GET operation in the bundled API specs against a local HTTP stub and asserting the captured request matches expectations. Delivers confidence in read correctness with no dependency on write behaviour.

**Acceptance Scenarios**:

1. **Given** the gateway is configured with the bundled `pets` and `bookstore` API specs, **When** `call_operation` is called with a GET operation and path/query parameters, **Then** the outbound HTTP request uses the GET method, the path parameters are interpolated into the URL, and the query parameters appear in the query string.
2. **Given** a GET operation with an optional query parameter omitted, **When** `call_operation` is invoked without that parameter, **Then** the outbound request does not include the parameter in the query string.
3. **Given** a GET operation targeting a nested resource (e.g. reviews for a book), **When** `call_operation` is called with all required path parameters, **Then** the URL reflects the full nested path with both IDs correctly substituted.

---

### User Story 2 - Mutating Operations Produce Correct HTTP Requests When Confirmed (Priority: P2)

A developer needs assurance that POST, PUT, PATCH, and DELETE operations send the right HTTP method, URL, and request body — and only do so when `confirmed=true` is passed.

**Why this priority**: Mutating operations carry the risk of unintended side effects. Verifying both the write-gate behaviour and the correctness of the outbound request is critical for safety and correctness.

**Independent Test**: Can be verified by invoking `call_operation` with `confirmed=true` and `confirmed=false` for each mutating operation and asserting the stub either receives the expected request or receives nothing.

**Acceptance Scenarios**:

1. **Given** a POST operation with a JSON request body, **When** `call_operation` is called with `confirmed=true`, **Then** the outbound request uses the POST method, the correct URL, and the body is serialised as JSON with the correct `Content-Type` header.
2. **Given** a PUT operation replacing a resource, **When** `call_operation` is called with `confirmed=true` and a full body, **Then** the outbound request uses the PUT method and the body matches the supplied parameters.
3. **Given** a DELETE operation, **When** `call_operation` is called with `confirmed=true`, **Then** the outbound request uses the DELETE method and no body is sent.
4. **Given** any mutating operation, **When** `call_operation` is called with `confirmed=false`, **Then** no HTTP request is made and a human-readable confirmation prompt is returned.
5. **Given** the PATCH operation using RFC 6902 JSON Patch, **When** `call_operation` is called with `confirmed=true` and a patch array, **Then** the outbound request uses the PATCH method and the `Content-Type` header reflects `application/json-patch+json`.

---

### User Story 3 - Discovery Tools Return Correct Metadata for All Loaded Specs (Priority: P3)

A developer needs confidence that `list_services`, `list_operations`, and `get_operation_detail` accurately reflect the operations defined in the API specs so that agents can navigate and discover APIs correctly.

**Why this priority**: Correct discovery is a prerequisite for agents invoking the right operations, but regressions here are less immediately dangerous than incorrect HTTP requests.

**Independent Test**: Can be verified by calling each discovery tool and asserting that the returned data matches the expected service names, operation IDs, tags, parameters, and schemas derived from the bundled specs.

**Acceptance Scenarios**:

1. **Given** the gateway is loaded with two services (pets, bookstore), **When** `list_services` is called, **Then** both services are returned with their names and descriptions.
2. **Given** the bookstore service has operations tagged with `books` and `reviews`, **When** `list_operations` is called with the `books` tag filter, **Then** only book operations are returned.
3. **Given** the `createPet` operation has a required body field `name`, **When** `get_operation_detail` is called for that operation, **Then** the returned schema includes `name` as a required field.

---

### Edge Cases

- What happens when a path parameter value contains special characters that require URL encoding?
- How does the test handle operations that return non-2xx responses from the stub (verify the gateway forwards the response correctly)?
- What happens when the request body is empty for a DELETE operation — does the gateway omit the body entirely?
- How does the PATCH operation's non-standard content type (`application/json-patch+json`) get set — is it derived from the spec or hard-coded?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The test suite MUST start an in-process HTTP stub server that records all inbound requests before each test run, so assertions can be made without network dependencies.
- **FR-002**: The test suite MUST load the gateway using the existing `pets` and `bookstore` API spec files, pointing base URLs at the stub server.
- **FR-003**: The test suite MUST invoke each MCP tool (`list_services`, `list_operations`, `get_operation_detail`, `call_operation`) via the same interface that external callers use, not by calling internal functions directly.
- **FR-004**: For every operation defined in the `pets` spec (listPets, createPet, getPetById, updatePet, deletePet), the test suite MUST verify the resulting outbound HTTP request matches the expected method, URL, and body.
- **FR-005**: For every operation defined in the `bookstore` spec (listBooks, createBook, getBookById, replaceBook, patchBook, deleteBook, listReviews, createReview), the test suite MUST verify the resulting outbound HTTP request matches the expected method, URL, and body.
- **FR-006**: The test suite MUST verify that mutating operations are blocked (no HTTP request sent) when `confirmed=false`.
- **FR-007**: The test suite MUST verify that read operations are always executed regardless of the `confirmed` value.
- **FR-008**: The test suite MUST verify that path parameters are correctly interpolated into the URL path and not included in query parameters or the request body.
- **FR-009**: The test suite MUST verify that optional query parameters are omitted from the outbound request when not supplied by the caller.
- **FR-010**: The test suite MUST verify that the `Content-Type` header on mutating requests matches the content type declared in the API spec for that operation.

### Key Entities

- **Stub Server**: An in-process HTTP server that captures and stores all incoming requests for assertion; responds with minimal valid responses to prevent errors in the gateway's response handling.
- **Test Case**: A pairing of (service, operationId, input parameters, expected HTTP request shape) that can be verified independently.
- **Expected HTTP Request**: The method, URL path, query string, headers, and body that the gateway MUST produce for a given operation invocation.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of operations defined across both bundled API specs are covered by at least one test case.
- **SC-002**: Each test case runs to completion in under 1 second, keeping the full suite fast enough to run on every commit.
- **SC-003**: A deliberate mutation to the URL-building or body-serialisation logic causes at least one test to fail, demonstrating the suite catches regressions.
- **SC-004**: The test suite requires no external network access — all HTTP traffic is intercepted by the local stub — so it runs reliably in any environment including CI.
- **SC-005**: Adding a new operation to an existing API spec and re-running the suite without adding a corresponding test case produces a visible gap (failing or skipped test), guiding developers to extend coverage.

## Assumptions

- The two bundled API spec files (`apis/pets.yaml` and `apis/bookstore.yaml`) are the primary test fixtures; no additional specs need to be created for this feature.
- The test suite will be written using Go's standard `testing` package and `net/http/httptest`, consistent with Go idiomatic practices and requiring no new test framework dependencies.
- The stub server will return a minimal valid response (e.g., an empty JSON object or array with a 200/201/204 status) sufficient to prevent the gateway from returning an error, without simulating real business logic.
- Service-level confirmation configuration (from feature 001) may affect whether `confirmed` is required; tests will use configurations where confirmation is required for mutating operations to exercise the write gate.
- Header filtering (`ignore_headers`) and static auth headers are out of scope for this test suite; all tests use a minimal configuration with no special header rules.
