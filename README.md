# Portier

An MCP (Model Context Protocol) API gateway that reads OpenAPI specs at startup and exposes them as four MCP tools, enabling LLM agents to progressively discover and call REST APIs.

## Why Portier?

Exposing REST APIs to LLM agents presents a hard trade-off: give the agent one tool per endpoint and you saturate the context window with hundreds of tool definitions; give it one generic tool and you lose type safety and discoverability.

Portier solves this with a **progressive discovery pattern**. The agent starts with a lightweight service list, drills into operations by tag, fetches a full schema only for the operation it intends to call, then executes — four focused round-trips instead of one massive upfront dump.

Other benefits:

- **No code generation.** Drop in an OpenAPI spec and you're done.
- **Write gate.** Mutating operations (POST, PUT, PATCH, DELETE) require an explicit `confirmed=true`, giving the agent — and humans in the loop — a natural pause point.
- **Allow lists.** Restrict which operations each service exposes so the agent can't call endpoints it shouldn't know about.
- **Two transports.** Run as a k8s sidecar over HTTP or wire directly into local agents.
- **OpenTelemetry.** All four tool handlers and outbound HTTP calls emit spans, so you can trace what the agent called and why.

## Architecture

```
Agent (LLM)
   │
   │  MCP JSON-RPC (stdio or HTTP)
   ▼
┌─────────────────────────────────────────────┐
│                  Portier                     │
│                                             │
│  list_services ──► Registry                 │
│  list_operations      │                     │
│  get_operation_detail │  parsed OpenAPI     │
│  call_operation  ◄────┘  specs in memory   │
│       │                                     │
│  write gate (confirmed=true required)       │
│  static auth headers injected              │
└──────────────────┬──────────────────────────┘
                   │  HTTP
                   ▼
         Upstream REST APIs
```

### The four MCP tools

| Tool | Purpose |
|------|---------|
| `list_services` | Returns service names, descriptions, and tags — the starting point for discovery |
| `list_operations(service, tag?)` | Lists operations in a service; optional tag filter narrows the result |
| `get_operation_detail(service, operationId)` | Returns full parameter and request/response schemas for one operation |
| `call_operation(service, operationId, params, confirmed)` | Executes the HTTP call; `confirmed` must be `true` for mutating methods |

### Registry

All OpenAPI specs are parsed at startup into an in-memory registry keyed by service name and then by operationId. Reads are concurrent (protected by `sync.RWMutex`); writes happen only at startup.

### Schema flattening

`$ref`, `allOf`, `oneOf`, `anyOf`, and nested objects are resolved into flat JSON maps the LLM can reason over. Circular references are bounded by a depth limit of 8 levels.

### Response truncation

Array responses are truncated to 20 items before being returned to the agent to keep context usage predictable.

## Configuration

```yaml
server:
  addr: ":8080"
  name: "my-mcp-gateway"
  transport: "http"   # "http" (streamable HTTP) or "stdio"
  telemetry:
    enabled: true
    endpoint: "localhost:4317"   # OTLP gRPC collector
    sample_ratio: 1.0            # 0.0-1.0

services:
  # Minimal — host and base path come from the spec's servers block.
  - name: pets
    spec: ./specs/pets.yaml

  # Override target host (e.g. point to staging).
  - name: bookstore
    spec: ./specs/bookstore.yaml
    host: https://staging.api.internal

  # Static auth headers — server-side only, never shown to the LLM.
  # ${ENV_VAR} is substituted at load time.
  - name: payments
    spec: ./specs/payments.yaml
    host: https://api.internal
    base_path: /payments/v2
    headers:
      Authorization: "Bearer ${PAYMENTS_API_TOKEN}"
      X-Tenant-Id: "acme-prod"
    ignore_headers:
      - Authorization   # managed via static headers above

  # Allow list — hides all other operations from the agent.
  - name: pets
    spec: ./specs/pets.yaml
    allow_operations:
      - listPets
      - getOwnerById
```

## Usage

### As a standalone server

```bash
# Build
go build -o portier ./cmd/portier

# Run (default config: config.yaml)
./portier

# Or point to a specific config
./portier /etc/portier/config.yaml
```

### As a Go library

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

### Claude Desktop (stdio transport)

Set `transport: "stdio"` in `config.yaml`, then add Portier to your Claude Desktop config:

```json
{
  "mcpServers": {
    "portier": {
      "command": "/path/to/portier",
      "args": ["/path/to/config.yaml"]
    }
  }
}
```

### Kubernetes (HTTP transport)

Use `transport: "http"` (the default). Deploy as a sidecar or standalone pod and point your MCP client at `http://<pod>:8080`.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go` | MCP protocol SDK |
| `github.com/getkin/kin-openapi` | OpenAPI spec parsing |
| `gopkg.in/yaml.v3` | Config parsing |
| `go.opentelemetry.io/...` | Distributed tracing |
