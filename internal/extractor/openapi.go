// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// Schema is a single kind extracted from a Kubernetes OpenAPI v2 swagger
// document. Schema is owned by the returned instance; the transforms mutate
// it in place, so callers should treat it as single-use.
type Schema struct {
	Group   string
	Version string
	Kind    string
	Schema  map[string]any
}

// ExtractOpenAPI walks a Kubernetes OpenAPI v2 swagger document and returns
// one Schema per x-kubernetes-group-version-kind entry with all $refs inlined
// and the standalone-strict transforms applied. The returned slice is sorted
// by (Group, Version, Kind) so golden tests and archive listings are stable
// across runs. Errors are aggregated: a malformed definition does not stop
// extraction of the rest.
func ExtractOpenAPI(data []byte) ([]Schema, []error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, []error{fmt.Errorf("decode swagger: %w", err)}
	}

	root, ok := doc.(map[string]any)
	if !ok {
		return nil, []error{fmt.Errorf("swagger document is not a JSON object")}
	}

	definitions, ok := root["definitions"].(map[string]any)
	if !ok {
		return nil, []error{fmt.Errorf("swagger document has no 'definitions'")}
	}

	// Visit definitions in name order so log/error ordering is stable.
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		out  []Schema
		errs []error
	)
	for _, name := range names {
		def, ok := definitions[name].(map[string]any)
		if !ok {
			continue
		}
		gvks := readGVKs(def)
		if len(gvks) == 0 {
			continue
		}
		for _, gvk := range gvks {
			schema, err := buildSchema(name, def, definitions)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			out = append(out, Schema{
				Group:   gvk.group,
				Version: gvk.version,
				Kind:    gvk.kind,
				Schema:  schema,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Kind < out[j].Kind
	})

	return out, errs
}

const (
	jsonNullType  = "null"
	jsonSchemaURI = "http://json-schema.org/schema#"
)

type gvk struct {
	group   string
	version string
	kind    string
}

func readGVKs(def map[string]any) []gvk {
	raw, ok := def["x-kubernetes-group-version-kind"].([]any)
	if !ok {
		return nil
	}
	var out []gvk
	for _, entry := range raw {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		g, _ := m["group"].(string)
		v, _ := m["version"].(string)
		k, _ := m["kind"].(string)
		if k == "" || v == "" {
			continue
		}
		out = append(out, gvk{group: g, version: v, kind: k})
	}
	return out
}

// buildSchema runs the standalone-strict transform pipeline on a single
// top-level definition and returns the transformed schema. Step ordering is
// significant: inlining must precede GVK injection (otherwise injected props
// would go through ref resolution) and vendor-extension stripping must run
// last so earlier steps can still read preserve-unknown-fields.
func buildSchema(name string, def map[string]any, defs map[string]any) (map[string]any, error) {
	// 1. Deep-copy (number-preserving).
	cloned, err := deepCopyJSON(def)
	if err != nil {
		return nil, fmt.Errorf("deep copy: %w", err)
	}
	schema, ok := cloned.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("definition is not an object")
	}

	// 2. Inline $refs.
	inlined, ok := inlineRefs(schema, defs, map[string]bool{name: true}).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("inlined definition is not an object")
	}

	// 3. Inject apiVersion / kind into top-level properties + required.
	injectGVK(inlined)

	// 4. Rewrite int-or-string nodes.
	inlined, _ = replaceIntOrString(inlined).(map[string]any)

	// 5. Nullable-optional for every object with properties.
	nullableOptional(inlined)

	// 6. additionalProperties:false including the root, so extra top-level
	//    keys alongside apiVersion/kind/metadata/spec/... are rejected.
	addAdditionalPropertiesFalse(inlined, false)

	// 7. Strip remaining x-kubernetes-* extensions (keep preserve-unknown-fields).
	stripVendorExtensions(inlined)

	// 8. Inject $schema so editors auto-detect.
	inlined["$schema"] = jsonSchemaURI

	return inlined, nil
}

// deepCopyJSON returns a deep copy of a value decoded with json.Decoder.UseNumber.
// The copy preserves json.Number values.
func deepCopyJSON(v any) (any, error) {
	switch n := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, val := range n {
			c, err := deepCopyJSON(val)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	case []any:
		out := make([]any, len(n))
		for i, val := range n {
			c, err := deepCopyJSON(val)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	default:
		return v, nil
	}
}

// inlineRefs walks node and replaces every {$ref: "#/definitions/X"} object
// with a deep-copy of defs[X]. Siblings of $ref follow the merge rules in
// plans/extract-k8s.md: metadata siblings (description, default, example,
// x-kubernetes-*) win over the inlined definition; structural siblings
// (type, properties, items, etc.) are overridden by the inlined definition.
// Cycles bail out to {type: object, x-kubernetes-preserve-unknown-fields: true}.
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
	next := copyVisiting(visiting)
	next[name] = true
	resolved := inlineRefs(clone, defs, next)

	m, ok := resolved.(map[string]any)
	if !ok {
		return resolved
	}
	return overlaySiblings(m, node)
}

func unresolvedPlaceholder(name string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": fmt.Sprintf("unresolved $ref: %s", name),
	}
}

func copyVisiting(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src)+1)
	maps.Copy(out, src)
	return out
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

func isStructuralKey(k string) bool {
	switch k {
	case "type", "properties", "items", "required",
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

// injectGVK adds apiVersion and kind to the top-level properties if missing
// and adds them to required. metadata is left as-is: absent kinds stay absent,
// and inlined metadata is neither promoted to required nor demoted.
func injectGVK(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		schema["properties"] = props
	}

	required := toStringSlice(schema["required"])

	if _, ok := props["apiVersion"]; !ok {
		props["apiVersion"] = map[string]any{
			"type":        "string",
			"description": "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
		}
	}
	if !containsString(required, "apiVersion") {
		required = append(required, "apiVersion")
	}

	if _, ok := props["kind"]; !ok {
		props["kind"] = map[string]any{
			"type":        "string",
			"description": "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
		}
	}
	if !containsString(required, "kind") {
		required = append(required, "kind")
	}

	schema["required"] = toAnySlice(required)
}

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

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

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
		props, _ := n["properties"].(map[string]any)
		if props != nil {
			required := toStringSlice(n["required"])
			for name, prop := range props {
				propMap, ok := prop.(map[string]any)
				if !ok {
					continue
				}
				if !containsString(required, name) {
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
			if k == "properties" {
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

// stripVendorExtensions removes every x-kubernetes-* key from the tree except
// x-kubernetes-preserve-unknown-fields, which carries structural meaning for
// downstream validators.
func stripVendorExtensions(node any) {
	switch n := node.(type) {
	case map[string]any:
		for k := range n {
			if !strings.HasPrefix(k, "x-kubernetes-") {
				continue
			}
			if k == "x-kubernetes-preserve-unknown-fields" {
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
