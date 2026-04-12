# Data Model: MCP Integration Test Suite

The integration test introduces no persistent data or new domain entities. All structures are test-local and exist only during a test run.

---

## Test Fixture: `callCase`

Represents a single `call_operation` test case — one pairing of (service, operation, input params) → expected outbound HTTP request.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Human-readable label, used as the `t.Run` subtest name |
| `service` | string | Service name as registered (e.g. `"pets"`, `"bookstore"`) |
| `operationID` | string | Operation ID from the OpenAPI spec (e.g. `"listPets"`) |
| `params` | map[string]any | Input parameters passed to `CallOperation` (flat: path + query + body merged) |
| `confirmed` | bool | Value of the `confirmed` flag passed to `CallOperation` |
| `wantMethod` | string | Expected HTTP method on the captured request (e.g. `"GET"`, `"POST"`) |
| `wantPath` | string | Expected URL path with path params substituted (e.g. `"/pets/abc-123"`) |
| `wantQuery` | string | Expected raw query string, empty string if none (e.g. `"limit=5"`) |
| `wantBody` | map[string]any | Expected JSON body deserialized, nil if no body expected |
| `wantBlocked` | bool | True when the write gate should block the call (confirmed=false on mutating op) |

---

## Test Fixture: `opCase`

Represents a single `list_operations` or `get_operation_detail` test case.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Subtest label |
| `service` | string | Service name |
| `tag` | string | Tag filter for `list_operations`; empty means no filter |
| `operationID` | string | Operation ID for `get_operation_detail`; empty for list tests |
| `wantOpIDs` | []string | For list tests: expected operation IDs in the result set |
| `wantField` | string | For detail tests: a top-level field that must be present (e.g. `"parameters"`) |
| `wantRequired` | []string | For detail tests: parameter names expected to be required |

---

## Stub Server State

Captured by the stub's handler closure; reset between test cases.

| Field | Type | Description |
|-------|------|-------------|
| `method` | string | HTTP method of the last received request |
| `path` | string | URL path of the last received request |
| `rawQuery` | string | Raw query string of the last received request |
| `body` | []byte | Raw body bytes of the last received request |
| `contentType` | string | `Content-Type` header of the last received request |

---

## Operations Coverage Map

Lists every operation from the two bundled specs that a test case must cover.

### `pets` service (`apis/pets.yaml`)

| Operation ID | Method | Path | Mutating |
|---|---|---|---|
| `listPets` | GET | `/pets` | No |
| `createPet` | POST | `/pets` | Yes |
| `getPetById` | GET | `/pets/{petId}` | No |
| `updatePet` | PUT | `/pets/{petId}` | Yes |
| `deletePet` | DELETE | `/pets/{petId}` | Yes |

### `bookstore` service (`apis/bookstore.yaml`)

| Operation ID | Method | Path | Mutating |
|---|---|---|---|
| `listBooks` | GET | `/books` | No |
| `createBook` | POST | `/books` | Yes |
| `getBookById` | GET | `/books/{bookId}` | No |
| `replaceBook` | PUT | `/books/{bookId}` | Yes |
| `patchBook` | PATCH | `/books/{bookId}` | Yes |
| `deleteBook` | DELETE | `/books/{bookId}` | Yes |
| `listReviews` | GET | `/books/{bookId}/reviews` | No |
| `createReview` | POST | `/books/{bookId}/reviews` | Yes |

Total: **13 operations**, each requiring at least one `callCase` entry. Mutating operations require an additional `wantBlocked=true` case.
