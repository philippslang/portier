# Quickstart: Running the Integration Tests

## Prerequisites

- Go 1.23+
- The repository checked out at `github.com/philippslang/portier`
- No external services required

## Run the full test suite

```bash
go test ./...
```

## Run only the integration tests

```bash
go test -v -run "TestListServices|TestListOperations|TestGetOperationDetail|TestCallOperation" ./...
```

## Run a single operation's test case

```bash
go test -v -run "TestCallOperation/createPet" ./...
```

## Run with race detector

```bash
go test -race ./...
```

## What the tests do

1. Start an in-process HTTP stub at `127.0.0.1:<random-port>` that records all inbound requests and returns `HTTP 200 {}`.
2. Load the `pets` and `bookstore` API specs from `apis/`, overriding their base URLs to point at the stub.
3. For each test case, invoke the relevant `Registry` method and assert the captured HTTP request matches expectations.
4. For mutating operations with `confirmed=false`, assert no request was made and the write-gate response is returned.

## Adding coverage for a new operation

When a new operation is added to an existing spec:

1. Open `integration_test.go`.
2. Find the table for `TestCallOperation`.
3. Add a row with the new `operationID`, expected method, path, and params.
4. If the operation is mutating, add a second row with `confirmed: false` and `wantBlocked: true`.
5. Run `go test ./...` to confirm the new case passes.
