package portier

import "github.com/getkin/kin-openapi/openapi3"

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
		for k, v := range merged {
			if k == "properties" {
				existing, _ := out["properties"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for pk, pv := range v.(map[string]any) {
					existing[pk] = pv
				}
				out["properties"] = existing
			} else if k == "required" {
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
