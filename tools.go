package portier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RegisterTools wires the 4 progressive discovery tools to an MCP server.
// Called automatically by NewServer; exposed so callers can embed portier tools
// in an existing MCP server alongside their own tools.
//
// The write gate (confirmed parameter) is controlled per-service via each
// ServiceConfig.RequireConfirmation field. Services with RequireConfirmation=true
// require the agent to pass confirmed=true for mutating operations; services with
// RequireConfirmation=false execute mutations immediately. The confirmed parameter
// is always present in the call_operation tool schema.
func RegisterTools(s *mcpserver.MCPServer, reg *Registry) {
	// Tool 1: list_services — no parameters
	listServicesTool := mcp.NewTool("list_services",
		mcp.WithDescription("List all available API services and their capabilities. Returns name, description, and tags for each service."),
	)
	s.AddTool(listServicesTool, withTracing("list_services", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return toJSONResult(reg.ListServices())
	}))

	// Tool 2: list_operations — service (required), tag (optional)
	listOpsTool := mcp.NewTool("list_operations",
		mcp.WithDescription("List operations available in a service. Returns operationId, summary, HTTP method, and tags. Optionally filter by tag."),
		mcp.WithString("service",
			mcp.Required(),
			mcp.Description("Service name from list_services"),
		),
		mcp.WithString("tag",
			mcp.Description("Optional tag to filter operations by category"),
		),
	)
	s.AddTool(listOpsTool, withTracing("list_operations", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		service, _ := args["service"].(string)
		tag, _ := args["tag"].(string)
		result, err := reg.ListOperations(service, tag)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return toJSONResult(result)
	}))

	// Tool 3: get_operation_detail — service + operationId (both required)
	detailTool := mcp.NewTool("get_operation_detail",
		mcp.WithDescription("Get full parameter schemas, request body schema, and response schema for an operation. Use this to understand how to call an operation before calling it."),
		mcp.WithString("service",
			mcp.Required(),
			mcp.Description("Service name"),
		),
		mcp.WithString("operationId",
			mcp.Required(),
			mcp.Description("Operation ID from list_operations"),
		),
	)
	s.AddTool(detailTool, withTracing("get_operation_detail", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		service, _ := args["service"].(string)
		opID, _ := args["operationId"].(string)
		result, err := reg.GetOperationDetail(service, opID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return toJSONResult(result)
	}))

	// Tool 4: call_operation — executes the API call; write gate is per-service
	callToolOpts := []mcp.ToolOption{
		mcp.WithDescription("Execute an API operation. Pass all path, query, and body parameters as a flat object in 'params'. Mutating operations (POST/PUT/PATCH/DELETE) on services that require confirmation will return a confirmation prompt unless confirmed=true."),
		mcp.WithString("service",
			mcp.Required(),
			mcp.Description("Service name"),
		),
		mcp.WithString("operationId",
			mcp.Required(),
			mcp.Description("Operation ID to execute"),
		),
		mcp.WithObject("params",
			mcp.Description("Path, query, and body parameters merged into a flat object"),
		),
		mcp.WithBoolean("confirmed",
			mcp.Description("Set to true to confirm a mutating (POST/PUT/PATCH/DELETE) operation on services that require confirmation"),
		),
	}
	callTool := mcp.NewTool("call_operation", callToolOpts...)
	s.AddTool(callTool, withTracing("call_operation", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		service, _ := args["service"].(string)
		opID, _ := args["operationId"].(string)

		confirmed, _ := args["confirmed"].(bool)

		params := map[string]any{}
		if p, ok := args["params"].(map[string]any); ok {
			params = p
		}

		result, err := reg.CallOperation(service, opID, params, confirmed)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if status, ok := result["status"].(string); ok && status == "confirmation_required" {
			span := trace.SpanFromContext(ctx)
			span.AddEvent("write_gate.confirmation_required",
				trace.WithAttributes(
					attribute.String("mcp.service", service),
					attribute.String("mcp.operation_id", opID),
				),
			)
		}

		return toJSONResult(result)
	}))
}

// toJSONResult serializes any value to a JSON text result for MCP.
func toJSONResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("JSON marshal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
