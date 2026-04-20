// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

// addAdditionalPropertiesFalse recursively walks the schema and, for every
// object that declares "properties" but not "additionalProperties", sets
// "additionalProperties": false. Two nodes are left alone:
//   - the root, when skipRoot is true;
//   - any node carrying "x-kubernetes-preserve-unknown-fields": true, which
//     marks a subtree where the API server accepts arbitrary keys (status
//     subresources, free-form map fields like HelmRelease.spec.values).
//     Forcing additionalProperties: false there rejects valid documents.
func addAdditionalPropertiesFalse(node any, skipRoot bool) {
	m, ok := node.(map[string]any)
	if !ok {
		if arr, ok := node.([]any); ok {
			for _, v := range arr {
				addAdditionalPropertiesFalse(v, false)
			}
		}
		return
	}
	if preserve, _ := m["x-kubernetes-preserve-unknown-fields"].(bool); preserve {
		return
	}
	if _, hasProps := m["properties"]; hasProps && !skipRoot {
		if _, hasAP := m["additionalProperties"]; !hasAP {
			m["additionalProperties"] = false
		}
	}
	for _, v := range m {
		addAdditionalPropertiesFalse(v, false)
	}
}

// replaceIntOrString replaces any object that represents a Kubernetes
// "int-or-string" value with {"oneOf": [{"type": "string"}, {"type": "integer"}]}.
// Two representations are recognised:
//   - legacy OpenAPI form: {"format": "int-or-string"};
//   - structural schema form: {"x-kubernetes-int-or-string": true}.
func replaceIntOrString(node any) any {
	switch n := node.(type) {
	case map[string]any:
		if isIntOrString(n) {
			return map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			}
		}
		for k, v := range n {
			n[k] = replaceIntOrString(v)
		}
		return n
	case []any:
		for i, v := range n {
			n[i] = replaceIntOrString(v)
		}
		return n
	default:
		return n
	}
}

func isIntOrString(m map[string]any) bool {
	if f, ok := m["format"].(string); ok && f == "int-or-string" {
		return true
	}
	if b, ok := m["x-kubernetes-int-or-string"].(bool); ok && b {
		return true
	}
	return false
}
