# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Portier is a Go MCP (Model Context Protocol) API gateway. It reads OpenAPI specs at startup and exposes them as four MCP tools so LLM agents can progressively discover and call REST APIs.

The module path is `github.com/philippslang/portier`.

## Layout

```
doc.go               # package portier — package-level doc comment
config.go            # Config types and LoadConfig
registry.go          # Registry, LoadSpec, and the 4 tool handler methods
schema.go            # flattenSchema* — OpenAPI → LLM-readable maps
telemetry.go         # OTel tracer init and withTracing middleware
tools.go             # RegisterTools — wires the 4 tools to the MCP server
server.go            # Server, NewServer, NewServerFromFile, Run
util.go              # isMutating, containsTag, filterIgnoredHeaders, truncateResponse
cmd/portier/main.go  # package main — CLI entrypoint
specs/               # Example OpenAPI specs
go.mod / go.sum      # module: github.com/philippslang/portier
```

## Build & Run

This project uses trunk-based development. Do not create feature branches.

```bash
# Build the CLI binary
go build -o portier ./cmd/portier

# Run (config path is the only argument, default: config.yaml)
./portier config.yaml

# Or run directly
go run ./cmd/portier config.yaml
```

There are no tests and no linter configuration currently.

## Library usage

```go
import "github.com/philippslang/portier"

// From a config file
srv, err := portier.NewServerFromFile("config.yaml")
srv.Run(ctx)

// Programmatically
cfg, err := portier.LoadConfig("config.yaml")
srv, err := portier.NewServer(cfg)
srv.Run(ctx)

// Embed just the tools in your own MCP server
reg := portier.NewRegistry(nil)
reg.LoadSpec(portier.ServiceConfig{Name: "petstore", SpecPath: "petstore.yaml"})
portier.RegisterTools(myMCPServer, reg)

// Add your own tools alongside portier's
srv.MCPServer().AddTool(myTool, myHandler)
srv.Run(ctx)
```

## Architecture

### The Four MCP Tools (progressive discovery pattern)

1. **`list_services`** — returns service names, descriptions, tags
2. **`list_operations(service, tag?)`** — lists operations in a service; optional tag filter
3. **`get_operation_detail(service, operationId)`** — full parameter/request/response schemas
4. **`call_operation(service, operationId, params, confirmed)`** — executes the HTTP call

### Write Gate

Mutating methods (POST, PUT, PATCH, DELETE) require `confirmed=true`. If `confirmed=false`, the tool returns a human-readable confirmation prompt instead of executing — enforced in the `call_operation` handler.

### Registry

`Registry` holds all parsed OpenAPI specs in memory (keyed by service name, then operationId). It's populated at startup by `LoadSpec()` calls — one per service config entry. Thread-safe with `sync.RWMutex`.

### Configuration-driven access control

`config.yaml` controls everything: which specs to load, host/base path overrides (for staging vs. prod), static auth headers (server-side only, never exposed to the LLM), headers to strip from schemas (`ignore_headers`), and per-service operation allow lists (`allow_operations`).

Static headers support `${ENV_VAR}` substitution at load time.

### Schema flattening

`flattenSchema()` resolves `$ref`, `allOf`/`oneOf`/`anyOf`, and nested objects into flat JSON maps the LLM can read. Circular references are guarded by a depth limit (max 8 levels). This is used by `get_operation_detail` to build the tool description returned to the agent.

### Response truncation

`call_operation` truncates JSON array responses to 20 items by default to keep LLM context manageable.

### Transports

- `transport: "http"` — streamable HTTP on the configured address (default `:8080`), suitable for k8s
- `transport: "stdio"` — reads/writes MCP JSON-RPC on stdin/stdout, suitable for direct Claude Desktop integration

### OpenTelemetry

Optional distributed tracing via OTLP gRPC. Configured under `server.telemetry` in `config.yaml`. All four MCP tool handlers and outbound HTTP calls are instrumented with spans.

## Active Technologies
- Go 1.23 (module `github.com/philippslang/portier`) + `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3`, `go.opentelemetry.io/otel` (001-service-level-confirmation)
- N/A — in-memory registry only (001-service-level-confirmation)
- Go 1.23 + `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3` (already in `go.mod`); `net/http/httptest` (stdlib — no new dependency) (003-integration-test-mcp)

## Recent Changes
- 001-service-level-confirmation: Added Go 1.23 (module `github.com/philippslang/portier`) + `github.com/mark3labs/mcp-go`, `github.com/getkin/kin-openapi/openapi3`, `go.opentelemetry.io/otel`
