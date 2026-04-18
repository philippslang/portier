package portier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// Operation is a single parsed OpenAPI operation.
type Operation struct {
	OperationID string `json:"operationId"`
	Summary     string `json:"summary"`
	// Description is the OpenAPI operation description, captured at load time
	// so search_operations can match against it without re-parsing the spec.
	Description string                   `json:"-"`
	Method      string                   `json:"method"`
	Path        string                   `json:"path"`
	Tags        []string                 `json:"tags"`
	Parameters  openapi3.Parameters      `json:"-"`
	RequestBody *openapi3.RequestBodyRef `json:"-"`
	Responses   *openapi3.Responses      `json:"-"`
}

// Service is a registered API service with its parsed operations.
type Service struct {
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	BaseURL             string                `json:"-"`
	Operations          map[string]*Operation // keyed by operationId
	Tags                []string              `json:"tags"`
	StaticHeaders       map[string]string     // injected into every request, never shown to LLM
	RequireConfirmation bool                  // resolved effective value; true = write gate active for this service
}

// Registry holds all parsed OpenAPI specs in memory. Thread-safe.
type Registry struct {
	mu         sync.RWMutex
	services   map[string]*Service
	httpClient *http.Client
}

// NewRegistry creates a Registry. Pass nil to use http.DefaultClient.
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

	requireConfirmation := true
	if cfg.RequireConfirmation != nil {
		requireConfirmation = *cfg.RequireConfirmation
	}

	svc := &Service{
		Name:                cfg.Name,
		Description:         doc.Info.Description,
		BaseURL:             baseURL,
		Operations:          make(map[string]*Operation),
		StaticHeaders:       cfg.Headers,
		RequireConfirmation: requireConfirmation,
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
				Description: op.Description,
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
	specHost := ""
	specBasePath := ""
	if len(doc.Servers) > 0 {
		serverURL := doc.Servers[0].URL
		serverURL = strings.TrimRight(serverURL, "/")
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

// countOperations returns the total number of operations with an operationId in a spec.
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

// ListServices returns name, description, and tags for every registered service.
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

// ListOperations returns operationId, summary, method, and tags for all operations
// in a service. Passing a non-empty tag filters to that tag only.
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
			"operationId":          op.OperationID,
			"summary":              op.Summary,
			"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
			"tags":                 op.Tags,
		})
	}
	return result, nil
}

// GetOperationDetail returns full parameter, request body, and response schemas
// for a single operation — flattened into maps the LLM can reason over.
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
		"operationId":          op.OperationID,
		"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
		"path":                 op.Path,
		"summary":              op.Summary,
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

// SearchOperations performs a case-insensitive substring search across
// every registered operation's path, summary, and description. Pass an empty
// services slice to search all registered services; pass a non-empty slice
// to scope the search — unknown names in the filter are collected into the
// response's unknownServices field rather than causing an error.
//
// Results are capped at 20 entries; when the cap is reached the response's
// truncated field is true and the agent should refine its query. The
// unknownServices key is only present when at least one supplied filter name
// was unrecognised.
func (r *Registry) SearchOperations(query string, services []string) (map[string]any, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, fmt.Errorf("query is required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var unknownServices []string
	var scope map[string]bool
	if len(services) > 0 {
		seen := make(map[string]bool, len(services))
		scope = make(map[string]bool, len(services))
		for _, name := range services {
			if seen[name] {
				continue
			}
			seen[name] = true
			if _, ok := r.services[name]; ok {
				scope[name] = true
			} else {
				unknownServices = append(unknownServices, name)
			}
		}
	}

	serviceNames := make([]string, 0, len(r.services))
	for name := range r.services {
		if scope != nil && !scope[name] {
			continue
		}
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	const resultCap = 20
	results := make([]map[string]any, 0, resultCap)
	truncated := false

outer:
	for _, name := range serviceNames {
		svc := r.services[name]
		opIDs := make([]string, 0, len(svc.Operations))
		for id := range svc.Operations {
			opIDs = append(opIDs, id)
		}
		sort.Strings(opIDs)
		for _, id := range opIDs {
			op := svc.Operations[id]
			if !strings.Contains(strings.ToLower(op.Path), needle) &&
				!strings.Contains(strings.ToLower(op.Summary), needle) &&
				!strings.Contains(strings.ToLower(op.Description), needle) {
				continue
			}
			results = append(results, map[string]any{
				"service":              svc.Name,
				"operationId":          op.OperationID,
				"method":               op.Method,
				"path":                 op.Path,
				"summary":              op.Summary,
				"confirmationRequired": svc.RequireConfirmation && isMutating(op.Method),
			})
			if len(results) >= resultCap {
				truncated = true
				break outer
			}
		}
	}

	resp := map[string]any{
		"results":   results,
		"truncated": truncated,
	}
	if len(unknownServices) > 0 {
		resp["unknownServices"] = unknownServices
	}
	return resp, nil
}

// CallOperation executes an API call. Mutating methods (POST/PUT/PATCH/DELETE)
// require confirmed=true; otherwise a confirmation prompt is returned instead.
func (r *Registry) CallOperation(
	serviceName, operationID string,
	params map[string]any,
	confirmed bool,
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

	// ── WRITE GATE ──────────────────────────────────────────────────────────
	if svc.RequireConfirmation && isMutating(op.Method) && !confirmed {
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

	// ── Build URL ────────────────────────────────────────────────────────────
	url := baseURL + op.Path

	// Substitute path params: /pets/{petId} → /pets/abc-123
	for _, pRef := range op.Parameters {
		p := pRef.Value
		if p.In == "path" {
			if val, ok := params[p.Name]; ok {
				url = strings.ReplaceAll(url, "{"+p.Name+"}", fmt.Sprintf("%v", val))
				delete(params, p.Name)
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

	// Header params
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

	// ── Response handling ────────────────────────────────────────────────────
	body, _ := io.ReadAll(resp.Body)

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		result = string(body)
	}

	result = truncateResponse(result, 20)

	return map[string]any{
		"status":     resp.StatusCode,
		"statusText": resp.Status,
		"data":       result,
	}, nil
}
