// Package portier implements a progressive REST API discovery MCP gateway.
//
// Architecture:
//   1. Loads N OpenAPI specs at startup into an in-memory registry (config-driven)
//   2. Exposes 4 MCP tools: list_services, list_operations, get_operation_detail, call_operation
//   3. Resolves operationId → method + URL + schema at call time
//   4. Enforces write gates on mutating HTTP methods
//   5. Supports streamable HTTP (for k8s) and stdio transports
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

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// Configuration — loaded from YAML
// =============================================================================

// Config is the top-level configuration loaded from config.yaml.
type Config struct {
	// Server settings
	Server ServerConfig `yaml:"server"`

	// Services to expose via MCP
	Services []ServiceConfig `yaml:"services"`
}

type ServerConfig struct {
	// Address to listen on (default ":8080")
	Addr string `yaml:"addr"`

	// Server name advertised via MCP (default "mcp-api-gateway")
	Name string `yaml:"name"`

	// Transport: "http" (default) or "stdio"
	Transport string `yaml:"transport"`

	// OpenTelemetry configuration
	Telemetry TelemetryConfig `yaml:"telemetry"`
}

type TelemetryConfig struct {
	// Enable tracing (default false)
	Enabled bool `yaml:"enabled"`

	// OTLP gRPC endpoint (default "localhost:4317")
	Endpoint string `yaml:"endpoint"`

	// Sample ratio 0.0-1.0 (default 1.0 = sample everything)
	SampleRatio float64 `yaml:"sample_ratio"`
}

type ServiceConfig struct {
	// Logical name for this service (used in list_services, list_operations, etc.)
	Name string `yaml:"name"`

	// Path to the OpenAPI spec file (YAML or JSON)
	SpecPath string `yaml:"spec"`

	// Override the target host for API calls.
	// If empty, uses the first server URL from the spec.
	Host string `yaml:"host,omitempty"`

	// Override the base path prepended to all operation paths.
	// If empty, uses the path from the spec's server URL.
	BasePath string `yaml:"base_path,omitempty"`

	// Allow list of operationIds. If non-empty, only these operations are exposed.
	// If empty/omitted, all operations in the spec are exposed.
	AllowOperations []string `yaml:"allow_operations,omitempty"`

	// Headers to ignore — these are stripped from tool schemas (not shown to the LLM)
	// and not sent in HTTP requests. Use for headers managed outside the agent
	// (e.g. auth headers injected by a gateway, or irrelevant tracing headers).
	// Case-insensitive matching.
	IgnoreHeaders []string `yaml:"ignore_headers,omitempty"`

	// Static headers injected into every HTTP request for this service.
	// These are never exposed to the LLM — they're server-side only.
	// Use for auth tokens, API keys, tenant IDs, etc.
	Headers map[string]string `yaml:"headers,omitempty"`
}

// LoadConfig reads and parses the YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Addr:      ":8080",
			Name:      "mcp-api-gateway",
			Transport: "http",
			Telemetry: TelemetryConfig{
				Endpoint:    "localhost:4317",
				SampleRatio: 1.0,
			},
		},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Validate
	if len(cfg.Services) == 0 {
		return nil, fmt.Errorf("config %s: no services defined", path)
	}
	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return nil, fmt.Errorf("config %s: service[%d] missing name", path, i)
		}
		if svc.SpecPath == "" {
			return nil, fmt.Errorf("config %s: service %q missing spec path", path, svc.Name)
		}
	}

	return cfg, nil
}

// =============================================================================
// Registry — holds parsed OpenAPI specs in memory
// =============================================================================

type Operation struct {
	OperationID string                   `json:"operationId"`
	Summary     string                   `json:"summary"`
	Method      string                   `json:"method"`
	Path        string                   `json:"path"`
	Tags        []string                 `json:"tags"`
	Parameters  openapi3.Parameters      `json:"-"`
	RequestBody *openapi3.RequestBodyRef `json:"-"`
	Responses   *openapi3.Responses      `json:"-"`
}

type Service struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	BaseURL       string                `json:"-"`
	Operations    map[string]*Operation // keyed by operationId
	Tags          []string              `json:"tags"`
	StaticHeaders map[string]string     // injected into every request, never shown to LLM
}

type Registry struct {
	mu         sync.RWMutex
	services   map[string]*Service
	httpClient *http.Client
}

func NewRegistry(httpClient *http.Client) *Registry {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Registry{
		services:   make(map[string]*Service),
		httpClient: httpClient,
	}
}

// LoadSpec parses one OpenAPI spec and registers it as a service.
// Call once per microservice at startup.
func (r *Registry) LoadSpec(cfg ServiceConfig) error {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(cfg.SpecPath)
	if err != nil {
		return fmt.Errorf("loading spec %s: %w", cfg.SpecPath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return fmt.Errorf("validating spec %s: %w", cfg.SpecPath, err)
	}

	// Resolve base URL: config overrides > spec servers block
	baseURL := resolveBaseURL(doc, cfg)

	svc := &Service{
		Name:          cfg.Name,
		Description:   doc.Info.Description,
		BaseURL:       baseURL,
		Operations:    make(map[string]*Operation),
		StaticHeaders: cfg.Headers,
	}

	// Build allow set for O(1) lookup. Empty means allow all.
	allowSet := make(map[string]bool, len(cfg.AllowOperations))
	for _, id := range cfg.AllowOperations {
		allowSet[id] = true
	}

	// Build ignore headers set (case-insensitive)
	ignoreHeaders := make(map[string]bool, len(cfg.IgnoreHeaders))
	for _, h := range cfg.IgnoreHeaders {
		ignoreHeaders[strings.ToLower(h)] = true
	}

	tagSet := map[string]bool{}

	for path, pathItem := range doc.Paths.Map() {
		for method, op := range pathItem.Operations() {
			if op.OperationID == "" {
				log.Printf("WARN: skipping %s %s in %s — no operationId", method, path, cfg.Name)
				continue
			}

			// Apply allow list filter
			if len(allowSet) > 0 && !allowSet[op.OperationID] {
				continue
			}

			// Filter out ignored headers from parameters
			filteredParams := filterIgnoredHeaders(op.Parameters, ignoreHeaders)

			svc.Operations[op.OperationID] = &Operation{
				OperationID: op.OperationID,
				Summary:     op.Summary,
				Method:      strings.ToUpper(method),
				Path:        path,
				Tags:        op.Tags,
				Parameters:  filteredParams,
				RequestBody: op.RequestBody,
				Responses:   op.Responses,
			}
			for _, t := range op.Tags {
				tagSet[t] = true
			}
		}
	}

	for t := range tagSet {
		svc.Tags = append(svc.Tags, t)
	}

	r.mu.Lock()
	r.services[cfg.Name] = svc
	r.mu.Unlock()

	totalInSpec := countOperations(doc)
	log.Printf("Loaded service %q: %d/%d operations (allow_list=%d), tags: %v",
		cfg.Name, len(svc.Operations), totalInSpec, len(allowSet), svc.Tags)
	return nil
}

// resolveBaseURL determines the target base URL from config overrides or the spec.
func resolveBaseURL(doc *openapi3.T, cfg ServiceConfig) string {
	// Start with whatever the spec declares
	specHost := ""
	specBasePath := ""
	if len(doc.Servers) > 0 {
		serverURL := doc.Servers[0].URL
		// Split into host and path: "https://api.example.com/v1" → host + /v1
		serverURL = strings.TrimRight(serverURL, "/")
		// Find the path portion after the scheme+host
		if idx := findPathStart(serverURL); idx > 0 {
			specHost = serverURL[:idx]
			specBasePath = serverURL[idx:]
		} else {
			specHost = serverURL
		}
	}

	host := specHost
	basePath := specBasePath

	if cfg.Host != "" {
		host = strings.TrimRight(cfg.Host, "/")
	}
	if cfg.BasePath != "" {
		basePath = "/" + strings.Trim(cfg.BasePath, "/")
	}

	return host + basePath
}

// findPathStart returns the index where the path begins in a URL like
// "https://api.example.com/v1/foo" → index of the third slash.
func findPathStart(u string) int {
	// Skip scheme "https://"
	slashes := 0
	for i, c := range u {
		if c == '/' {
			slashes++
			if slashes == 3 {
				return i
			}
		}
	}
	return -1
}

// countOperations returns the total number of operations in a spec.
func countOperations(doc *openapi3.T) int {
	n := 0
	for _, pathItem := range doc.Paths.Map() {
		for _, op := range pathItem.Operations() {
			if op.OperationID != "" {
				n++
			}
		}
	}
	return n
}

// =============================================================================
// Tool handlers — the 4 MCP tools
// =============================================================================

// Tool 1: list_services
// Returns: [{ name, description, tags }]
func (r *Registry) ListServices() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]map[string]any, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, map[string]any{
			"name":        svc.Name,
			"description": svc.Description,
			"tags":        svc.Tags,
		})
	}
	return result
}

// Tool 2: list_operations(service, tag?)
// Returns: [{ operationId, summary, method, tags }]
func (r *Registry) ListOperations(serviceName, tag string) ([]map[string]any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	svc, ok := r.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", serviceName)
	}

	result := make([]map[string]any, 0)
	for _, op := range svc.Operations {
		if tag != "" && !containsTag(op.Tags, tag) {
			continue
		}
		result = append(result, map[string]any{
			"operationId": op.OperationID,
			"summary":     op.Summary,
			"method":      op.Method,
			"tags":        op.Tags,
		})
	}
	return result, nil
}

// Tool 3: get_operation_detail(service, operationId)
// Returns: full param schemas, request body schema, response schema
func (r *Registry) GetOperationDetail(serviceName, operationID string) (map[string]any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	svc, ok := r.services[serviceName]
	if !ok {
		return nil, fmt.Errorf("unknown service: %s", serviceName)
	}
	op, ok := svc.Operations[operationID]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s.%s", serviceName, operationID)
	}

	detail := map[string]any{
		"operationId": op.OperationID,
		"method":      op.Method,
		"path":        op.Path,
		"summary":     op.Summary,
	}

	// Flatten parameters into a simple schema the LLM can read
	params := make([]map[string]any, 0)
	for _, pRef := range op.Parameters {
		p := pRef.Value
		pm := map[string]any{
			"name":     p.Name,
			"in":       p.In, // path | query | header
			"required": p.Required,
		}
		if p.Schema != nil && p.Schema.Value != nil {
			// For simple params, inline key fields directly.
			// For complex params (objects/arrays), flatten the full schema.
			sv := p.Schema.Value
			types := sv.Type.Slice()
			if len(types) == 1 && (types[0] == "object" || types[0] == "array") {
				pm["schema"] = flattenSchemaRef(p.Schema, 0)
			} else {
				if len(types) == 1 {
					pm["type"] = types[0]
				}
				if sv.Enum != nil {
					pm["enum"] = sv.Enum
				}
				if sv.Description != "" {
					pm["description"] = sv.Description
				}
				if sv.Format != "" {
					pm["format"] = sv.Format
				}
				if sv.Default != nil {
					pm["default"] = sv.Default
				}
			}
		}
		params = append(params, pm)
	}
	detail["parameters"] = params

	// Request body — extract and flatten JSON schema
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		if mt, ok := op.RequestBody.Value.Content["application/json"]; ok && mt.Schema != nil {
			detail["requestBody"] = flattenSchemaRef(mt.Schema, 0)
		}
	}

	// Response — just the 200/201 schema, flattened
	if op.Responses != nil {
		for code, respRef := range op.Responses.Map() {
			if code == "200" || code == "201" {
				if mt, ok := respRef.Value.Content["application/json"]; ok && mt.Schema != nil {
					detail["responseSchema"] = flattenSchemaRef(mt.Schema, 0)
				}
				break
			}
		}
	}

	return detail, nil
}

// Tool 4: call_operation(service, operationId, params)
// This is where the write gate lives.
func (r *Registry) CallOperation(
	serviceName, operationID string,
	params map[string]any,
	confirmed bool, // explicit confirmation flag from the agent
) (map[string]any, error) {

	r.mu.RLock()
	svc, ok := r.services[serviceName]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("unknown service: %s", serviceName)
	}
	op, ok := svc.Operations[operationID]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("unknown operation: %s.%s", serviceName, operationID)
	}
	baseURL := svc.BaseURL
	staticHeaders := svc.StaticHeaders
	r.mu.RUnlock()

	// ── WRITE GATE ──────────────────────────────────────────────────────
	// Hard gate: if method is mutating and not explicitly confirmed,
	// return a confirmation prompt instead of executing.
	if isMutating(op.Method) && !confirmed {
		return map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("This will %s %s. Confirm?", op.Method, op.Path),
			"operation": map[string]any{
				"service":     serviceName,
				"operationId": operationID,
				"method":      op.Method,
				"params":      params,
			},
		}, nil
	}

	// ── Build HTTP request ──────────────────────────────────────────────
	url := baseURL + op.Path

	// Substitute path params: /pets/{petId} → /pets/abc-123
	for _, pRef := range op.Parameters {
		p := pRef.Value
		if p.In == "path" {
			if val, ok := params[p.Name]; ok {
				url = strings.ReplaceAll(url, "{"+p.Name+"}", fmt.Sprintf("%v", val))
				delete(params, p.Name) // consumed
			}
		}
	}

	// Query params
	queryParts := make([]string, 0)
	for _, pRef := range op.Parameters {
		p := pRef.Value
		if p.In == "query" {
			if val, ok := params[p.Name]; ok {
				queryParts = append(queryParts, fmt.Sprintf("%s=%v", p.Name, val))
				delete(params, p.Name)
			}
		}
	}
	if len(queryParts) > 0 {
		url += "?" + strings.Join(queryParts, "&")
	}

	// Header params — extract from params map, apply after request is built
	headerParams := make(map[string]string)
	for _, pRef := range op.Parameters {
		p := pRef.Value
		if p.In == "header" {
			if val, ok := params[p.Name]; ok {
				headerParams[p.Name] = fmt.Sprintf("%v", val)
				delete(params, p.Name)
			}
		}
	}

	// Remaining params become the JSON body (for POST/PUT/PATCH)
	var bodyReader io.Reader
	if len(params) > 0 && isMutating(op.Method) {
		bodyBytes, _ := json.Marshal(params)
		bodyReader = strings.NewReader(string(bodyBytes))
	}

	req, err := http.NewRequest(op.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Inject static headers from config (auth, API keys, tenant IDs)
	for k, v := range staticHeaders {
		req.Header.Set(k, v)
	}

	// Inject dynamic header params from the LLM
	for k, v := range headerParams {
		req.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s %s: %w", op.Method, url, err)
	}
	defer resp.Body.Close()

	// ── Response handling with truncation ────────────────────────────────
	body, _ := io.ReadAll(resp.Body)

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		// Non-JSON response
		result = string(body)
	}

	// Truncate large array responses to keep LLM context lean
	result = truncateResponse(result, 20) // max 20 items in any array

	return map[string]any{
		"status":     resp.StatusCode,
		"statusText": resp.Status,
		"data":       result,
	}, nil
}

// =============================================================================
// OpenTelemetry — tracer initialization and tool handler middleware
// =============================================================================

const tracerName = "mcp-api-gateway"

// initTracer sets up the OTel trace provider with OTLP gRPC export.
// Returns a shutdown function that must be called on exit.
func initTracer(ctx context.Context, cfg TelemetryConfig, serverName string) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(), // use WithTLSCredentials in production
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRatio < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRatio)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serverName),
		)),
	)

	otel.SetTracerProvider(tp)
	log.Printf("OpenTelemetry tracing enabled → %s (sample_ratio=%.2f)", cfg.Endpoint, cfg.SampleRatio)
	return tp.Shutdown, nil
}

// ToolHandlerFunc is the mcp-go handler signature.
type ToolHandlerFunc = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)

// withTracing wraps a tool handler with an OTel span.
// Adds tool name, arguments, and error status as span attributes/events.
func withTracing(toolName string, handler ToolHandlerFunc) ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tracer := otel.Tracer(tracerName)
		ctx, span := tracer.Start(ctx, "tool."+toolName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("mcp.tool", toolName),
			),
		)
		defer span.End()

		// Add relevant arguments as span attributes
		args := request.GetArguments()
		if s, ok := args["service"].(string); ok {
			span.SetAttributes(attribute.String("mcp.service", s))
		}
		if op, ok := args["operationId"].(string); ok {
			span.SetAttributes(attribute.String("mcp.operation_id", op))
		}
		if confirmed, ok := args["confirmed"].(bool); ok {
			span.SetAttributes(attribute.Bool("mcp.confirmed", confirmed))
		}

		result, err := handler(ctx, request)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, "tool returned error")
			span.AddEvent("tool.error")
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return result, err
	}
}

// =============================================================================
// MCP server wiring — register the 4 tools
// =============================================================================

// RegisterTools wires the 4 progressive discovery tools to an MCP server.
// Called automatically by NewServer; exposed so callers can embed portier tools
// in an existing MCP server alongside their own tools.
func RegisterTools(s *mcpserver.MCPServer, reg *Registry) {
	// Tool 1: list_services — no parameters
	listServicesTool := mcp.NewTool("list_services",
		mcp.WithDescription("List all available API services and their capabilities. Returns name, description, and tags for each service."),
	)
	s.AddTool(listServicesTool, withTracing("list_services", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result := reg.ListServices()
		return toJSONResult(result)
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

	// Tool 4: call_operation — executes the API call with write gate
	callTool := mcp.NewTool("call_operation",
		mcp.WithDescription("Execute an API operation. Pass all path, query, and body parameters as a flat object in 'params'. Mutating operations (POST/PUT/PATCH/DELETE) require confirmed=true or will return a confirmation prompt."),
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
			mcp.Description("Set to true to confirm a mutating (POST/PUT/PATCH/DELETE) operation"),
		),
	)
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

		// Record write gate events on the span
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

// =============================================================================
// Server — top-level handle for library and CLI use
// =============================================================================

// Server is a configured portier MCP gateway. Create one with NewServer,
// then call Run to start accepting connections.
type Server struct {
	cfg          *Config
	registry     *Registry
	mcp          *mcpserver.MCPServer
	otelShutdown func(context.Context) error // nil if telemetry disabled
}

// NewServer builds a Server from a Config. It loads all OpenAPI specs and
// initializes telemetry. Call Run to start serving.
func NewServer(cfg *Config) (*Server, error) {
	var otelShutdown func(context.Context) error
	if cfg.Server.Telemetry.Enabled {
		shutdown, err := initTracer(context.Background(), cfg.Server.Telemetry, cfg.Server.Name)
		if err != nil {
			return nil, fmt.Errorf("initializing tracing: %w", err)
		}
		otelShutdown = shutdown
	}

	httpClient := &http.Client{}
	if cfg.Server.Telemetry.Enabled {
		httpClient.Transport = otelhttp.NewTransport(http.DefaultTransport)
	}

	reg := NewRegistry(httpClient)
	for _, svcCfg := range cfg.Services {
		if err := reg.LoadSpec(svcCfg); err != nil {
			if otelShutdown != nil {
				_ = otelShutdown(context.Background())
			}
			return nil, fmt.Errorf("loading service %q: %w", svcCfg.Name, err)
		}
	}

	s := mcpserver.NewMCPServer(
		cfg.Server.Name,
		"1.0.0",
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
	)
	RegisterTools(s, reg)

	return &Server{
		cfg:          cfg,
		registry:     reg,
		mcp:          s,
		otelShutdown: otelShutdown,
	}, nil
}

// NewServerFromFile loads a YAML config file and creates a Server.
func NewServerFromFile(path string) (*Server, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return NewServer(cfg)
}

// Run starts the configured transport and blocks until the server exits.
func (s *Server) Run(ctx context.Context) error {
	if s.otelShutdown != nil {
		defer func() {
			if err := s.otelShutdown(context.Background()); err != nil {
				log.Printf("Error shutting down tracer: %v", err)
			}
		}()
	}

	if s.cfg.Server.Transport == "stdio" {
		log.Printf("Starting MCP server %q over stdio", s.cfg.Server.Name)
		return mcpserver.ServeStdio(s.mcp)
	}

	httpServer := mcpserver.NewStreamableHTTPServer(s.mcp)
	log.Printf("Starting MCP server %q on %s (streamable HTTP)", s.cfg.Server.Name, s.cfg.Server.Addr)
	return httpServer.Start(s.cfg.Server.Addr)
}

// Registry returns the underlying service registry for direct programmatic access.
func (s *Server) Registry() *Registry { return s.registry }

// MCPServer returns the underlying MCP server, allowing additional tools to be
// registered before calling Run.
func (s *Server) MCPServer() *mcpserver.MCPServer { return s.mcp }

// =============================================================================
// Helpers
// =============================================================================

// =============================================================================
// Schema flattening — resolve $ref pointers into inline schemas for the LLM
// =============================================================================

const maxFlattenDepth = 8 // prevent infinite recursion on circular $refs

// flattenSchemaRef resolves a SchemaRef (which may be a $ref pointer) into
// a plain map[string]any with all references inlined. Depth-limited to avoid
// blowing up on circular references (e.g. TreeNode -> children -> []TreeNode).
func flattenSchemaRef(ref *openapi3.SchemaRef, depth int) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{"type": "object"}
	}
	if depth >= maxFlattenDepth {
		return map[string]any{
			"type":       "object",
			"_truncated": true,
			"_reason":    "max depth reached — possible circular reference",
		}
	}
	return flattenSchema(ref.Value, depth)
}

// flattenSchema converts an openapi3.Schema into a clean map the LLM can read.
// Handles: objects, arrays, allOf/oneOf/anyOf, primitives, enums.
func flattenSchema(s *openapi3.Schema, depth int) map[string]any {
	out := map[string]any{}

	// Type — kin-openapi v0.127+ uses *Types (slice), earlier uses string
	if s.Type != nil {
		types := s.Type.Slice()
		if len(types) == 1 {
			out["type"] = types[0]
		} else if len(types) > 1 {
			out["type"] = types
		}
	}

	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Enum != nil {
		out["enum"] = s.Enum
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if s.Example != nil {
		out["example"] = s.Example
	}
	if s.Nullable {
		out["nullable"] = true
	}
	if s.ReadOnly {
		out["readOnly"] = true
	}
	if s.Min != nil {
		out["minimum"] = *s.Min
	}
	if s.Max != nil {
		out["maximum"] = *s.Max
	}
	if s.MinLength != 0 {
		out["minLength"] = s.MinLength
	}
	if s.MaxLength != nil {
		out["maxLength"] = *s.MaxLength
	}
	if s.Pattern != "" {
		out["pattern"] = s.Pattern
	}

	// Required fields
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}

	// Object — recurse into properties
	if s.Properties != nil {
		props := map[string]any{}
		for name, propRef := range s.Properties {
			props[name] = flattenSchemaRef(propRef, depth+1)
		}
		out["properties"] = props
	}

	// additionalProperties
	if s.AdditionalProperties.Has != nil && *s.AdditionalProperties.Has {
		if s.AdditionalProperties.Schema != nil {
			out["additionalProperties"] = flattenSchemaRef(s.AdditionalProperties.Schema, depth+1)
		} else {
			out["additionalProperties"] = true
		}
	}

	// Array — recurse into items
	if s.Items != nil {
		out["items"] = flattenSchemaRef(s.Items, depth+1)
	}

	// Composition: allOf / oneOf / anyOf
	if len(s.AllOf) > 0 {
		merged := mergeAllOf(s.AllOf, depth+1)
		// Merge allOf result into this schema (allOf typically extends a base)
		for k, v := range merged {
			if k == "properties" {
				// Merge property maps
				existing, _ := out["properties"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for pk, pv := range v.(map[string]any) {
					existing[pk] = pv
				}
				out["properties"] = existing
			} else if k == "required" {
				// Merge required arrays
				existingReq, _ := out["required"].([]string)
				for _, r := range v.([]string) {
					existingReq = append(existingReq, r)
				}
				out["required"] = existingReq
			} else if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	if len(s.OneOf) > 0 {
		variants := make([]map[string]any, len(s.OneOf))
		for i, ref := range s.OneOf {
			variants[i] = flattenSchemaRef(ref, depth+1)
		}
		out["oneOf"] = variants
	}
	if len(s.AnyOf) > 0 {
		variants := make([]map[string]any, len(s.AnyOf))
		for i, ref := range s.AnyOf {
			variants[i] = flattenSchemaRef(ref, depth+1)
		}
		out["anyOf"] = variants
	}

	return out
}

// mergeAllOf flattens an allOf list into a single merged schema map.
// This handles the common pattern of base type + extension.
func mergeAllOf(refs openapi3.SchemaRefs, depth int) map[string]any {
	merged := map[string]any{}
	mergedProps := map[string]any{}
	var mergedRequired []string

	for _, ref := range refs {
		flat := flattenSchemaRef(ref, depth)
		for k, v := range flat {
			switch k {
			case "properties":
				for pk, pv := range v.(map[string]any) {
					mergedProps[pk] = pv
				}
			case "required":
				if reqs, ok := v.([]string); ok {
					mergedRequired = append(mergedRequired, reqs...)
				}
			default:
				merged[k] = v
			}
		}
	}

	if len(mergedProps) > 0 {
		merged["properties"] = mergedProps
	}
	if len(mergedRequired) > 0 {
		merged["required"] = mergedRequired
	}
	return merged
}

func isMutating(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// filterIgnoredHeaders removes header parameters that match the ignore set.
// This strips them at load time so they never appear in tool schemas.
func filterIgnoredHeaders(params openapi3.Parameters, ignoreSet map[string]bool) openapi3.Parameters {
	if len(ignoreSet) == 0 {
		return params
	}
	filtered := make(openapi3.Parameters, 0, len(params))
	for _, pRef := range params {
		if pRef.Value != nil && pRef.Value.In == "header" {
			if ignoreSet[strings.ToLower(pRef.Value.Name)] {
				continue
			}
		}
		filtered = append(filtered, pRef)
	}
	return filtered
}

// truncateResponse caps arrays to maxItems to keep LLM context lean.
// Returns a wrapper with total count + truncated slice.
func truncateResponse(v any, maxItems int) any {
	switch val := v.(type) {
	case []any:
		if len(val) > maxItems {
			return map[string]any{
				"totalCount":    len(val),
				"returnedCount": maxItems,
				"items":         val[:maxItems],
				"truncated":     true,
			}
		}
	case map[string]any:
		// Recurse into map values looking for arrays
		for k, child := range val {
			val[k] = truncateResponse(child, maxItems)
		}
	}
	return v
}
