// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux-schema/internal/yamldoc"
)

// ExtractCRDs reads a YAML payload and returns one Schema per CRD version
// found. Errors are aggregated: a failure on one document or version does
// not stop extraction of the rest.
func ExtractCRDs(data []byte) ([]Schema, []error) {
	var out []Schema
	var errs []error
	docIndex := 0
	for _, raw := range yamldoc.Split(data) {
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

	return collectCRDs(m), nil
}

// collectCRDs walks a parsed document and returns every CustomResourceDefinition
// found, descending into List-style containers via the "items" field.
func collectCRDs(m map[string]any) []map[string]any {
	var out []map[string]any
	if kind, _ := m["kind"].(string); kind == "CustomResourceDefinition" {
		out = append(out, m)
	}
	if items, ok := m["items"].([]any); ok {
		for _, it := range items {
			if child, ok := it.(map[string]any); ok {
				out = append(out, collectCRDs(child)...)
			}
		}
	}
	return out
}

// versionsFromCRD extracts each version's openAPIV3Schema and applies the
// OpenAPI→JSON Schema transformations.
func versionsFromCRD(crd map[string]any) ([]Schema, []error) {
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

	var out []Schema
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

		closeAdditionalPropertiesChildren(schema)
		transformed, _ := replaceIntOrString(schema).(map[string]any)

		out = append(out, Schema{
			Group:   group,
			Version: versionName,
			Kind:    kind,
			JSON:    transformed,
		})
	}
	return out, errs
}
