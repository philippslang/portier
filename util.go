package portier

import (
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

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
		for k, child := range val {
			val[k] = truncateResponse(child, maxItems)
		}
	}
	return v
}
