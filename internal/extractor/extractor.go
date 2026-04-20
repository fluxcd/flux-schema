// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package extractor converts Kubernetes CustomResourceDefinition YAML into
// per-version JSON Schema documents. It accepts bare CRDs, List-wrapped CRDs,
// and multi-document YAML (e.g. the output of `kubectl get crds -o yaml`).
package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"
)

// CRD is a single CRD version with its transformed openAPIV3Schema.
// Schema is owned by the CRD instance; the transforms mutate it in place,
// so callers should treat it as single-use.
type CRD struct {
	Group   string
	Kind    string
	Version string
	Schema  map[string]any
}

// Extract reads a YAML payload and returns one CRD per CRD version found.
// Errors are aggregated: a failure on one document or version does not stop
// extraction of the rest.
func Extract(data []byte) ([]CRD, []error) {
	var out []CRD
	var errs []error
	docIndex := 0
	for _, raw := range splitYAMLDocs(data) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		docIndex++
		docs, err := parseDocument(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("document %d: %w", docIndex, err))
			continue
		}
		for _, crd := range docs {
			crds, cerrs := versionsFromCRD(crd)
			out = append(out, crds...)
			for _, cerr := range cerrs {
				errs = append(errs, fmt.Errorf("document %d: %w", docIndex, cerr))
			}
		}
	}
	return out, errs
}

// splitYAMLDocs splits a YAML payload on "\n---" separators,
// also recognizing a leading "---" at the start of the buffer.
func splitYAMLDocs(data []byte) [][]byte {
	data = bytes.TrimPrefix(data, []byte("---\n"))
	return bytes.Split(data, []byte("\n---"))
}

// parseDocument decodes a single YAML document and returns the CRDs it contains.
// A top-level document that is not a mapping is an error.
func parseDocument(raw []byte) ([]map[string]any, error) {
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()

	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("JSON decode error: %w", err)
	}

	m, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document is not a YAML mapping")
	}

	return extractCRDs(m), nil
}

// extractCRDs walks a parsed document and returns every CustomResourceDefinition
// found, descending into List-style containers via the "items" field.
func extractCRDs(m map[string]any) []map[string]any {
	var out []map[string]any
	if kind, _ := m["kind"].(string); kind == "CustomResourceDefinition" {
		out = append(out, m)
	}
	if items, ok := m["items"].([]any); ok {
		for _, it := range items {
			if child, ok := it.(map[string]any); ok {
				out = append(out, extractCRDs(child)...)
			}
		}
	}
	return out
}

// versionsFromCRD extracts each version's openAPIV3Schema and applies the
// OpenAPI→JSON Schema transformations.
func versionsFromCRD(crd map[string]any) ([]CRD, []error) {
	spec, ok := crd["spec"].(map[string]any)
	if !ok {
		return nil, []error{fmt.Errorf("CRD missing 'spec'")}
	}
	names, _ := spec["names"].(map[string]any)
	kind, _ := names["kind"].(string)
	group, _ := spec["group"].(string)
	if kind == "" || group == "" {
		return nil, []error{fmt.Errorf("CRD missing spec.names.kind or spec.group")}
	}

	versions, ok := spec["versions"].([]any)
	if !ok || len(versions) == 0 {
		return nil, []error{fmt.Errorf("CRD %s has no spec.versions", kind)}
	}

	var out []CRD
	var errs []error
	for i, v := range versions {
		vm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		versionName, _ := vm["name"].(string)
		schemaHolder, _ := vm["schema"].(map[string]any)
		schema, _ := schemaHolder["openAPIV3Schema"].(map[string]any)
		if schema == nil {
			errs = append(errs, fmt.Errorf("version[%d] %q has no schema.openAPIV3Schema", i, versionName))
			continue
		}

		addAdditionalPropertiesFalse(schema, true)
		transformed, _ := replaceIntOrString(schema).(map[string]any)

		out = append(out, CRD{
			Group:   group,
			Kind:    kind,
			Version: versionName,
			Schema:  transformed,
		})
	}
	return out, errs
}
