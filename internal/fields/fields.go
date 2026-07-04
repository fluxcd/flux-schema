// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package fields

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// ScopeNamespaced identifies a namespaced Kubernetes kind.
	ScopeNamespaced = "Namespaced"
	// ScopeCluster identifies a cluster-scoped Kubernetes kind.
	ScopeCluster = "Cluster"
)

// Options controls field index generation for a single Kubernetes kind.
type Options struct {
	GVK   schema.GroupVersionKind
	Scope string
}

// Flatten decodes schemaJSON and returns its greppable field index.
func Flatten(schemaJSON []byte, opts Options) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(schemaJSON))
	decoder.UseNumber()

	var root any
	if err := decoder.Decode(&root); err != nil {
		return "", fmt.Errorf("decode schema JSON: %w", err)
	}

	rootMap, ok := root.(map[string]any)
	if !ok {
		return "", errors.New("schema root must be an object")
	}

	return FlattenMap(rootMap, opts)
}

// FlattenMap returns the greppable field index for root without mutating it.
func FlattenMap(root map[string]any, opts Options) (string, error) {
	if opts.GVK.Kind == "" {
		return "", errors.New("GVK kind is required")
	}
	if opts.GVK.Version == "" {
		return "", errors.New("GVK version is required")
	}

	var builder strings.Builder
	writeHeader(&builder, opts)
	if err := emit(&builder, root, "", true); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func writeHeader(builder *strings.Builder, opts Options) {
	builder.WriteString("apiVersion <string> enum=")
	builder.WriteString(opts.GVK.GroupVersion().String())
	builder.WriteByte('\n')

	builder.WriteString("kind <string> enum=")
	builder.WriteString(opts.GVK.Kind)
	builder.WriteByte('\n')

	builder.WriteString("metadata.name <string> (required)")
	builder.WriteByte('\n')

	switch opts.Scope {
	case ScopeNamespaced:
		builder.WriteString("metadata.namespace <string> (required)")
		builder.WriteByte('\n')
	case ScopeCluster:
	default:
		builder.WriteString("metadata.namespace <string>")
		builder.WriteByte('\n')
	}
}

func emit(builder *strings.Builder, root map[string]any, prefix string, skipRootMetadata bool) error {
	properties := schemaMap(root["properties"])
	if len(properties) == 0 {
		return nil
	}

	required := requiredSet(root["required"])
	names := make([]string, 0, len(properties))
	for name := range properties {
		if skipRootMetadata && (name == "apiVersion" || name == "kind" || name == "metadata") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		property, _ := properties[name].(map[string]any)
		path := prefix + name
		if err := writeLine(builder, path, property, required[name]); err != nil {
			return err
		}
		if err := emitChildren(builder, property, path); err != nil {
			return err
		}
	}

	return nil
}

func emitChildren(builder *strings.Builder, property map[string]any, path string) error {
	items := schemaMap(property["items"])
	if isSingleType(property, "array") && len(schemaMap(items["properties"])) > 0 {
		return emit(builder, items, path+"[].", false)
	}

	if len(schemaMap(property["properties"])) > 0 {
		return emit(builder, property, path+".", false)
	}

	additionalProperties := schemaMap(property["additionalProperties"])
	if isSingleType(property, "object") && len(schemaMap(additionalProperties["properties"])) > 0 {
		return emit(builder, additionalProperties, path+".<key>.", false)
	}

	return nil
}

func writeLine(builder *strings.Builder, path string, property map[string]any, required bool) error {
	builder.WriteString(path)
	builder.WriteByte(' ')
	builder.WriteString(typeString(property))

	if required {
		builder.WriteString(" (required)")
	}

	if values, ok := property["enum"].([]any); ok {
		builder.WriteString(" enum=")
		for i, value := range values {
			if i > 0 {
				builder.WriteByte('|')
			}
			stringValue, err := stringifyEnumValue(value)
			if err != nil {
				return fmt.Errorf("stringify enum for %s: %w", path, err)
			}
			builder.WriteString(stringValue)
		}
	}

	if value, ok := property["default"]; ok {
		stringValue, err := stringifyDefault(value)
		if err != nil {
			return fmt.Errorf("stringify default for %s: %w", path, err)
		}
		builder.WriteString(" default=")
		builder.WriteString(stringValue)
	}

	if description, ok := property["description"].(string); ok {
		if cleaned := cleanDescription(description); cleaned != "" {
			builder.WriteString("\t# ")
			builder.WriteString(cleaned)
		}
	}

	builder.WriteByte('\n')
	return nil
}

func requiredSet(value any) map[string]bool {
	required := make(map[string]bool)
	values, ok := value.([]any)
	if !ok {
		return required
	}
	for _, item := range values {
		name, ok := item.(string)
		if ok {
			required[name] = true
		}
	}
	return required
}

func schemaMap(value any) map[string]any {
	result, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return result
}
