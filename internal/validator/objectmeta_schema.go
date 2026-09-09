// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	jsonSchemaProperties = "properties"
	jsonNullType         = "null"
)

// augmentRootObjectMetaSchema mirrors kube-apiserver's implicit ObjectMeta
// handling in the in-memory schema used by validation. The loaded schema file
// is not rewritten.
func augmentRootObjectMetaSchema(schema map[string]any) {
	props, _ := schema[jsonSchemaProperties].(map[string]any)
	if props == nil {
		return
	}
	metadata, _ := props["metadata"].(map[string]any)
	if metadata == nil {
		return
	}
	if inlined, ok := inlineLocalRef(schema, metadata); ok {
		metadata = inlined
		props["metadata"] = metadata
	}
	metadataProps, _ := metadata[jsonSchemaProperties].(map[string]any)
	if metadataProps == nil {
		metadataProps = map[string]any{}
		metadata[jsonSchemaProperties] = metadataProps
	}
	if _, ok := metadata["type"]; !ok {
		metadata["type"] = "object"
	}
	if _, ok := metadata["additionalProperties"]; !ok {
		metadata["additionalProperties"] = false
	}
	for name, objectMetaProp := range objectMetaPropertySchemas() {
		existing, ok := metadataProps[name].(map[string]any)
		if !ok {
			if _, exists := metadataProps[name]; !exists {
				metadataProps[name] = objectMetaProp
			}
			continue
		}
		for key, value := range objectMetaProp {
			if _, exists := existing[key]; !exists {
				existing[key] = value
			}
		}
		if acceptsNull(objectMetaProp) {
			addNullType(existing)
		}
	}
}

func inlineLocalRef(root, node map[string]any) (map[string]any, bool) {
	ref, _ := node["$ref"].(string)
	if ref == "" {
		return nil, false
	}
	target, ok := resolveLocalRef(root, ref)
	if !ok {
		return nil, false
	}
	inlined := runtime.DeepCopyJSON(target)
	for key, value := range node {
		if key == "$ref" {
			continue
		}
		inlined[key] = runtime.DeepCopyJSONValue(value)
	}
	return inlined, true
}

func resolveLocalRef(root map[string]any, ref string) (map[string]any, bool) {
	pointer, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, false
	}
	var node any = root
	for _, segment := range strings.Split(pointer, "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		node, ok = m[unescapeJSONPointer(segment)]
		if !ok {
			return nil, false
		}
	}
	target, ok := node.(map[string]any)
	return target, ok
}

func unescapeJSONPointer(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	s = strings.ReplaceAll(s, "~0", "~")
	return s
}

func objectMetaPropertySchemas() map[string]map[string]any {
	props, _ := objectSchemaForGoType(reflect.TypeOf(metav1.ObjectMeta{}), nil)[jsonSchemaProperties].(map[string]any)
	out := make(map[string]map[string]any, len(props))
	for name, prop := range props {
		if propMap, ok := prop.(map[string]any); ok {
			out[name] = propMap
		}
	}
	return out
}

func schemaForGoType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	nullable := false
	for t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}

	schema := objectSchemaForGoType(t, seen)
	if nullable {
		addNullType(schema)
	}
	return schema
}

func objectSchemaForGoType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	t = derefGoType(t)
	if t == reflect.TypeOf(metav1.Time{}) || t == reflect.TypeOf(metav1.MicroTime{}) {
		return map[string]any{"type": []any{"string", jsonNullType}, "format": "date-time"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema := map[string]any{"type": "integer"}
		if t.Kind() == reflect.Int32 || t.Kind() == reflect.Int64 {
			schema["format"] = fmt.Sprintf("int%d", t.Bits())
		}
		return schema
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema := map[string]any{"type": "integer", "minimum": 0}
		if t.Kind() == reflect.Uint32 || t.Kind() == reflect.Uint64 {
			schema["format"] = fmt.Sprintf("int%d", t.Bits())
		}
		return schema
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": schemaForGoType(t.Elem(), seen)}
	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaForGoType(t.Elem(), seen),
		}
	case reflect.Struct:
		return objectSchemaForGoStruct(t, seen)
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{"type": "object"}
	}
}

func objectSchemaForGoStruct(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	if seen == nil {
		seen = map[reflect.Type]bool{}
	}
	if seen[t] {
		return map[string]any{"type": "object"}
	}
	nextSeen := maps.Clone(seen)
	nextSeen[t] = true

	props := map[string]any{}
	var required []any
	for i := range t.NumField() {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		name, opts, skip := jsonField(field)
		if skip {
			continue
		}
		if slices.Contains(opts, "inline") {
			for nestedName, nestedSchema := range schemaPropertiesForGoType(field.Type, nextSeen) {
				props[nestedName] = nestedSchema
			}
			continue
		}
		fieldSchema := schemaForGoType(field.Type, nextSeen)
		if optionalJSONField(opts) {
			addNullType(fieldSchema)
		}
		props[name] = fieldSchema
		if !optionalJSONField(opts) {
			required = append(required, name)
		}
	}

	schema := map[string]any{"type": "object"}
	if len(props) > 0 {
		schema[jsonSchemaProperties] = props
		schema["additionalProperties"] = false
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func optionalJSONField(opts []string) bool {
	return slices.Contains(opts, "omitempty") || slices.Contains(opts, "omitzero")
}

func schemaPropertiesForGoType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	schema := objectSchemaForGoType(t, seen)
	props, _ := schema[jsonSchemaProperties].(map[string]any)
	return props
}

func jsonField(field reflect.StructField) (string, []string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", nil, true
	}
	if tag == "" {
		return field.Name, nil, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	return name, parts[1:], false
}

func derefGoType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func addNullType(schema map[string]any) {
	typeValue, ok := schema["type"]
	if !ok {
		schema["type"] = []any{"object", jsonNullType}
		return
	}
	switch typed := typeValue.(type) {
	case string:
		if typed != jsonNullType {
			schema["type"] = []any{typed, jsonNullType}
		}
	case []any:
		if !slices.Contains(typed, jsonNullType) {
			schema["type"] = append(typed, jsonNullType)
		}
	}
}

func acceptsNull(schema map[string]any) bool {
	switch typed := schema["type"].(type) {
	case string:
		return typed == jsonNullType
	case []any:
		return slices.Contains(typed, jsonNullType)
	default:
		return false
	}
}
