// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

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
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
				JSON:    schema,
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

const jsonSchemaURI = "http://json-schema.org/schema#"

func readGVKs(def map[string]any) []GVK {
	raw, ok := def["x-kubernetes-group-version-kind"].([]any)
	if !ok {
		return nil
	}
	var out []GVK
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
		out = append(out, GVK{Group: g, Version: v, Kind: k})
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
	closeAdditionalProperties(inlined)

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
