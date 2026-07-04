// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// k8sSkipKindRe matches kind names that are not GitOps-applicable.
var k8sSkipKindRe = regexp.MustCompile(`^(WatchEvent|[A-Za-z]+Options)$`)

var swaggerOperations = []string{"get", "put", "post", "delete", "patch", "head", "options"}

// ExtractKubernetes walks a Kubernetes OpenAPI v2 swagger document and returns
// one Schema per x-kubernetes-group-version-kind entry with all $refs inlined
// and the standalone-strict transforms applied. The returned slice is sorted
// by (Group, Version, Kind) so golden tests and archive listings are stable
// across runs. Errors are aggregated: a malformed definition does not stop
// extraction of the rest.
func ExtractKubernetes(data []byte) ([]Schema, []error) {
	root, definitions, names, errs := parseSwaggerDocument(data)
	if errs != nil {
		return nil, errs
	}
	scopes := scopeFromPaths(root)
	source := kubernetesSource(root)

	var out []Schema
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
			if k8sSkipKindRe.MatchString(gvk.Kind) {
				continue
			}
			schema, err := buildSchema(name, def, definitions)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			out = append(out, Schema{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
				Scope:   scopes[gvk],
				Source:  source,
				JSON:    schema,
			})
		}
	}

	sortSchemasByGVK(out)
	return out, errs
}

// parseSwaggerDefinitions decodes a swagger document and returns its
// definitions map together with the definition names sorted alphabetically.
// Number-preserving decode (UseNumber) lets integer literals round-trip
// through downstream transforms unchanged.
func parseSwaggerDefinitions(data []byte) (map[string]any, []string, []error) {
	_, definitions, names, errs := parseSwaggerDocument(data)
	return definitions, names, errs
}

// parseSwaggerDocument decodes a swagger document and returns its root object,
// definitions map, and definition names sorted alphabetically.
func parseSwaggerDocument(data []byte) (map[string]any, map[string]any, []string, []error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, nil, []error{fmt.Errorf("decode swagger: %w", err)}
	}

	root, ok := doc.(map[string]any)
	if !ok {
		return nil, nil, nil, []error{fmt.Errorf("swagger document is not a JSON object")}
	}

	definitions, ok := root["definitions"].(map[string]any)
	if !ok {
		return nil, nil, nil, []error{fmt.Errorf("swagger document has no 'definitions'")}
	}

	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return root, definitions, names, nil
}

func kubernetesSource(root map[string]any) string {
	info, _ := root["info"].(map[string]any)
	version, _ := info["version"].(string)
	// The swagger checked into kubernetes/kubernetes release tags carries the
	// literal placeholder "unversioned"; treat it as no version information.
	if version == "unversioned" {
		version = ""
	}
	return sourceWithVersion("Kubernetes", version)
}

func sourceWithVersion(name, version string) string {
	if version == "" {
		return name
	}
	return name + " " + version
}

// scopeFromPaths derives resource scope from OpenAPI operation paths. A GVK is
// namespaced if any operation path contains /namespaces/{namespace}/; otherwise,
// a GVK seen only on non-namespaced paths is cluster-scoped.
func scopeFromPaths(root map[string]any) map[GVK]string {
	scopes := map[GVK]string{}
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return scopes
	}

	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		namespaced := strings.Contains(path, "/namespaces/{namespace}/")
		recordPathScope(scopes, item, namespaced)
		for _, operation := range swaggerOperations {
			op, ok := item[operation].(map[string]any)
			if !ok {
				continue
			}
			recordPathScope(scopes, op, namespaced)
		}
	}

	return scopes
}

func recordPathScope(scopes map[GVK]string, node map[string]any, namespaced bool) {
	gvk, ok := readOperationGVK(node)
	if !ok {
		return
	}
	if namespaced {
		scopes[gvk] = "Namespaced"
		return
	}
	if _, seen := scopes[gvk]; !seen {
		scopes[gvk] = "Cluster"
	}
}

func readOperationGVK(node map[string]any) (GVK, bool) {
	raw, ok := node["x-kubernetes-group-version-kind"].(map[string]any)
	if !ok {
		return GVK{}, false
	}
	g, _ := raw["group"].(string)
	v, _ := raw["version"].(string)
	k, _ := raw["kind"].(string)
	if k == "" || v == "" {
		return GVK{}, false
	}
	return GVK{Group: g, Version: v, Kind: k}, true
}

// sortSchemasByGVK orders schemas by (Group, Version, Kind) so output is
// deterministic across runs.
func sortSchemasByGVK(out []Schema) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Kind < out[j].Kind
	})
}

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

	// 7. Strip remaining x-kubernetes-* extensions (keep preserve-unknown-fields
	//    and validations).
	stripVendorExtensions(inlined)

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
