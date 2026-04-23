// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Functions in this file follow the buildSchema pipeline order:
// inlineRefs → injectGVK → replaceIntOrString → nullableOptional →
// closeAdditionalProperties → stripVendorExtensions. StripDescriptions is an
// optional post-process that callers can apply to trim documentation-only
// fields from the output.

const keyProperties = "properties"

// --- $ref inlining ---

// inlineRefs walks node and replaces every {$ref: "#/definitions/X"} object
// with a deep-copy of defs[X]. Siblings of $ref follow these merge rules:
// metadata siblings (description, default, example, x-kubernetes-*) win over
// the inlined definition; structural siblings (type, properties, items, etc.)
// are overridden by the inlined definition. Cycles bail out to
// {type: object, x-kubernetes-preserve-unknown-fields: true}.
func inlineRefs(node any, defs map[string]any, visiting map[string]bool) any {
	switch n := node.(type) {
	case map[string]any:
		if refVal, ok := n["$ref"].(string); ok {
			return resolveRef(n, refVal, defs, visiting)
		}
		for k, v := range n {
			n[k] = inlineRefs(v, defs, visiting)
		}
		return n
	case []any:
		for i, v := range n {
			n[i] = inlineRefs(v, defs, visiting)
		}
		return n
	default:
		return n
	}
}

// resolveRef returns the inlined replacement for a single $ref node. node is
// the host object that carries the $ref (its non-$ref siblings are merged
// onto the result via overlaySiblings); refVal is the raw $ref string; defs
// is the swagger document's "definitions" map; visiting is the set of
// definition names currently being resolved on this branch, used to detect
// cycles. Behavior:
//
//   - Malformed or non-#/definitions/ refs return an unresolvedPlaceholder.
//   - Refs into the visiting set bail out to a preserve-unknown-fields object
//     so the schema stays well-formed without infinite recursion.
//   - Targets that are missing or fail to deep-copy also degrade to a
//     placeholder rather than aborting the whole pipeline.
//   - On success, the target is deep-copied, recursively inlined with the
//     ref's name added to visiting, and returned with host siblings overlaid.
func resolveRef(node map[string]any, refVal string, defs map[string]any, visiting map[string]bool) any {
	const prefix = "#/definitions/"
	if !strings.HasPrefix(refVal, prefix) || len(refVal) == len(prefix) {
		return overlaySiblings(unresolvedPlaceholder(refVal), node)
	}
	name := strings.TrimPrefix(refVal, prefix)

	if visiting[name] {
		return map[string]any{
			"type":                                 "object",
			"x-kubernetes-preserve-unknown-fields": true,
		}
	}

	target, ok := defs[name]
	if !ok {
		return overlaySiblings(unresolvedPlaceholder(name), node)
	}

	clone, err := deepCopyJSON(target)
	if err != nil {
		return overlaySiblings(unresolvedPlaceholder(name), node)
	}
	next := maps.Clone(visiting)
	next[name] = true
	resolved := inlineRefs(clone, defs, next)

	m, ok := resolved.(map[string]any)
	if !ok {
		return resolved
	}
	return overlaySiblings(m, node)
}

// unresolvedPlaceholder returns a stand-in object used by resolveRef when a
// $ref cannot be inlined (malformed prefix, missing target, or deep-copy
// failure). The placeholder is a permissive object schema that keeps the
// surrounding document well-formed; the original ref name is recorded in
// the description so unresolved references are traceable in the output.
func unresolvedPlaceholder(name string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": fmt.Sprintf("unresolved $ref: %s", name),
	}
}

// overlaySiblings merges keys from the $ref host (other than $ref itself) onto
// the inlined node. Structural keys from the inlined node always win; metadata
// siblings (description, default, example, x-kubernetes-*, unknown keys) win
// from the host side.
func overlaySiblings(inlined map[string]any, host map[string]any) map[string]any {
	for k, v := range host {
		if k == "$ref" {
			continue
		}
		if isStructuralKey(k) {
			if _, has := inlined[k]; has {
				continue
			}
		}
		inlined[k] = v
	}
	return inlined
}

// --- GVK injection ---

// injectGVK adds apiVersion and kind to the top-level properties if missing
// and adds them to required. metadata is left as-is: absent kinds stay absent,
// and inlined metadata is neither promoted to required nor demoted.
func injectGVK(schema map[string]any) {
	props, _ := schema[keyProperties].(map[string]any)
	if props == nil {
		props = map[string]any{}
		schema[keyProperties] = props
	}

	required := toStringSlice(schema["required"])

	if _, ok := props["apiVersion"]; !ok {
		props["apiVersion"] = map[string]any{
			"type":        "string",
			"description": "APIVersion defines the versioned schema of this representation of an object.",
		}
	}
	if !slices.Contains(required, "apiVersion") {
		required = append(required, "apiVersion")
	}

	if _, ok := props["kind"]; !ok {
		props["kind"] = map[string]any{
			"type":        "string",
			"description": "Kind is a string value representing the REST resource this object represents.",
		}
	}
	if !slices.Contains(required, "kind") {
		required = append(required, "kind")
	}

	schema["required"] = toAnySlice(required)
}

// toStringSlice coerces a JSON-decoded array (typed as []any) into []string,
// silently dropping non-string elements. Used to read the "required" array
// off a schema node, where the round-trip through encoding/json gives back
// []any even though every element is a string in practice.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// toAnySlice converts a []string back into []any so it can be stored on a
// schema node and re-marshalled by encoding/json. Inverse of toStringSlice;
// used after mutating a "required" array.
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// --- int-or-string rewriting ---

// replaceIntOrString replaces any object that represents a Kubernetes
// "int-or-string" value with {"oneOf": [{"type": "string"}, {"type": "integer"}]}.
// Two representations are recognised:
//   - legacy OpenAPI form: {"format": "int-or-string"};
//   - structural schema form: {"x-kubernetes-int-or-string": true}.
//
// Metadata siblings (description, default, …) are preserved on the rewritten
// node; structural siblings that would contradict the oneOf (properties, items,
// enum, …) are dropped defensively in case the input has been hand-edited.
func replaceIntOrString(node any) any {
	switch n := node.(type) {
	case map[string]any:
		if isIntOrString(n) {
			out := map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			}
			for k, v := range n {
				if k == "x-kubernetes-int-or-string" || isStructuralKey(k) {
					continue
				}
				out[k] = v
			}
			return out
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

// isIntOrString reports whether m represents a Kubernetes int-or-string
// value, in either the legacy OpenAPI form (format: "int-or-string") or
// the structural-schema form (x-kubernetes-int-or-string: true).
func isIntOrString(m map[string]any) bool {
	if f, ok := m["format"].(string); ok && f == "int-or-string" {
		return true
	}
	if b, ok := m["x-kubernetes-int-or-string"].(bool); ok && b {
		return true
	}
	return false
}

// --- nullable-optional marking ---

const jsonNullType = "null"

// nullableOptional walks every object with properties and marks each property
// that is not in required as nullable: scalar type becomes ["<t>", "null"],
// list types gain "null" when missing, oneOf gets a {type: null} branch.
// The transform is idempotent. Subtrees under x-kubernetes-preserve-unknown-fields
// are skipped entirely.
func nullableOptional(node any) {
	switch n := node.(type) {
	case map[string]any:
		if preserve, _ := n["x-kubernetes-preserve-unknown-fields"].(bool); preserve {
			return
		}
		props, _ := n[keyProperties].(map[string]any)
		if props != nil {
			required := toStringSlice(n["required"])
			for name, prop := range props {
				propMap, ok := prop.(map[string]any)
				if !ok {
					continue
				}
				if !slices.Contains(required, name) {
					markNullable(propMap)
				}
				// Recurse into the property's subtree so nested objects are
				// processed with their own required array. Done here (not
				// via the generic walk below) to avoid visiting the
				// properties map twice.
				nullableOptional(propMap)
			}
		}
		for k, v := range n {
			if k == keyProperties {
				continue
			}
			nullableOptional(v)
		}
	case []any:
		for _, v := range n {
			nullableOptional(v)
		}
	}
}

// markNullable adds "null" as an accepted shape on a single property node,
// matching the JSON Schema convention for an optional field. Three cases:
//   - type is a string: rewrite to ["<t>", "null"];
//   - type is a list:   append "null" if not already present;
//   - oneOf is set:     append a {type: "null"} branch if not already present.
//
// The transform is idempotent and leaves allOf/anyOf-only nodes alone, since
// adding null there changes the validation semantics rather than relaxing them.
func markNullable(prop map[string]any) {
	// type handling.
	if t, ok := prop["type"]; ok {
		switch tv := t.(type) {
		case string:
			if tv == jsonNullType {
				return
			}
			prop["type"] = []any{tv, jsonNullType}
			return
		case []any:
			for _, item := range tv {
				if s, ok := item.(string); ok && s == jsonNullType {
					return
				}
			}
			prop["type"] = append(tv, jsonNullType)
			return
		}
	}
	// oneOf handling.
	if oneOf, ok := prop["oneOf"].([]any); ok {
		for _, branch := range oneOf {
			bm, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := bm["type"].(string); ok && s == jsonNullType {
				return
			}
		}
		prop["oneOf"] = append(oneOf, map[string]any{"type": jsonNullType})
		return
	}
	// allOf/anyOf or otherwise unclassifiable: leave alone.
}

// --- additionalProperties closing ---

// closeAdditionalProperties walks node and sets additionalProperties:false on
// every object that declares "properties" but doesn't already set it. Subtrees
// under x-kubernetes-preserve-unknown-fields:true are skipped because the API
// server accepts arbitrary keys there (status subresources, free-form map
// fields like HelmRelease.spec.values); forcing additionalProperties:false
// would reject valid documents.
func closeAdditionalProperties(node any) {
	switch n := node.(type) {
	case map[string]any:
		if preserve, _ := n["x-kubernetes-preserve-unknown-fields"].(bool); preserve {
			return
		}
		if _, hasProps := n[keyProperties]; hasProps {
			if _, hasAP := n["additionalProperties"]; !hasAP {
				n["additionalProperties"] = false
			}
		}
		for _, v := range n {
			closeAdditionalProperties(v)
		}
	case []any:
		for _, v := range n {
			closeAdditionalProperties(v)
		}
	}
}

// closeAdditionalPropertiesChildren is closeAdditionalProperties but leaves
// the root object's own additionalProperties untouched. Used by the CRD
// pipeline, where the openAPIV3Schema describes a custom resource whose root
// often omits the apiVersion/kind/metadata wrapper added by the API server;
// closing the root would reject that wrapper.
func closeAdditionalPropertiesChildren(node any) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if preserve, _ := m["x-kubernetes-preserve-unknown-fields"].(bool); preserve {
		return
	}
	for _, v := range m {
		closeAdditionalProperties(v)
	}
}

// --- vendor extension stripping ---

// stripVendorExtensions removes x-kubernetes-* keys from the tree except those
// that carry validation semantics: x-kubernetes-preserve-unknown-fields (keeps
// subtrees open for server-side unknown fields) and x-kubernetes-validations
// (CEL rules enforced by the API server).
func stripVendorExtensions(node any) {
	switch n := node.(type) {
	case map[string]any:
		for k := range n {
			if !strings.HasPrefix(k, "x-kubernetes-") {
				continue
			}
			if keepVendorExtension(k) {
				continue
			}
			delete(n, k)
		}
		for _, v := range n {
			stripVendorExtensions(v)
		}
	case []any:
		for _, v := range n {
			stripVendorExtensions(v)
		}
	}
}

func keepVendorExtension(k string) bool {
	switch k {
	case "x-kubernetes-preserve-unknown-fields",
		"x-kubernetes-validations":
		return true
	}
	return false
}

// --- description stripping ---

// StripDescriptions removes every "description" key from the tree. Descriptions
// are documentation-only and don't affect JSON Schema validation; dropping
// them trims the output substantially for native Kubernetes schemas, where
// descriptions make up the bulk of the payload. This is an optional
// post-process — the extraction pipeline does not call it automatically.
//
// Keys inside "properties", "patternProperties", "$defs", and "definitions"
// are user-defined property/schema names, not schema keywords, so a field
// literally named "description" (as JSONSchemaProps has) is preserved and
// only its metadata siblings underneath are stripped.
func StripDescriptions(node any) {
	switch n := node.(type) {
	case map[string]any:
		delete(n, "description")
		for k, v := range n {
			if isSchemaNameMap(k) {
				if m, ok := v.(map[string]any); ok {
					for _, child := range m {
						StripDescriptions(child)
					}
					continue
				}
			}
			StripDescriptions(v)
		}
	case []any:
		for _, v := range n {
			StripDescriptions(v)
		}
	}
}

// isSchemaNameMap reports whether the value under k is a map of user-defined
// names to subschemas, rather than a schema itself. Inside such maps, the keys
// are property or definition names and must not be confused with JSON Schema
// keywords like "description".
//
// "dependentSchemas" (Draft 2019-09+) is deliberately omitted: kube-openapi
// does not emit it, and CRDs reject it under structural-schema rules, so it
// never appears in this project's inputs.
func isSchemaNameMap(k string) bool {
	switch k {
	case keyProperties, "patternProperties", "$defs", "definitions":
		return true
	}
	return false
}

// --- shared across sections ---

// isStructuralKey reports whether k is a JSON Schema keyword that controls
// the shape or validation of a node, as opposed to metadata (description,
// default, example, vendor extensions). Used by overlaySiblings to decide
// which sibling keys of a $ref are overridden by the inlined definition,
// and by replaceIntOrString to drop contradicting keys when rewriting an
// int-or-string node into a oneOf.
func isStructuralKey(k string) bool {
	switch k {
	case "type", keyProperties, "items", "required",
		"oneOf", "allOf", "anyOf", "not",
		"additionalProperties",
		"enum", "format", "pattern",
		"minimum", "maximum",
		"minLength", "maxLength",
		"minItems", "maxItems", "uniqueItems",
		"minProperties", "maxProperties",
		"multipleOf":
		return true
	}
	return false
}
