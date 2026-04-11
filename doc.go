// Package portier implements a progressive REST API discovery MCP gateway.
//
// Architecture:
//  1. Loads N OpenAPI specs at startup into an in-memory registry (config-driven)
//  2. Exposes 4 MCP tools: list_services, list_operations, get_operation_detail, call_operation
//  3. Resolves operationId → method + URL + schema at call time
//  4. Enforces write gates on mutating HTTP methods
//  5. Supports streamable HTTP (for k8s) and stdio transports
//
// Library usage:
//
//	cfg, err := portier.LoadConfig("config.yaml")
//	srv, err := portier.NewServer(cfg)
//	srv.Run(ctx)
//
// Or build your own MCP server and embed just the tools:
//
//	reg := portier.NewRegistry(httpClient)
//	reg.LoadSpec(portier.ServiceConfig{Name: "petstore", SpecPath: "petstore.yaml"})
//	portier.RegisterTools(myMCPServer, reg)
//
// Dependencies:
//
//	github.com/mark3labs/mcp-go     — MCP protocol SDK
//	github.com/getkin/kin-openapi   — OpenAPI spec parsing
//	gopkg.in/yaml.v3                — Config parsing
package portier
