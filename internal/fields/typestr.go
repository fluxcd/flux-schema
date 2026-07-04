// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package fields

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const jsonTypeNull = "null"

var (
	newlineOrTab = regexp.MustCompile("[\n\t]+")
	spaces       = regexp.MustCompile(" +")
)

func typeString(schema map[string]any) string {
	if isIntOrString(schema) {
		return "<int-or-string>"
	}

	if isSingleType(schema, "array") {
		items := schemaMap(schema["items"])
		if itemType, ok := singleType(items); ok && itemType != "object" {
			return fmt.Sprintf("<[]%s>", itemType)
		}
		return "<[]object>"
	}

	if isSingleType(schema, "object") {
		additionalProperties := schemaMap(schema["additionalProperties"])
		if len(schemaMap(additionalProperties["properties"])) > 0 {
			return "<map[string]object>"
		}
		if valueType, ok := singleType(additionalProperties); ok {
			return fmt.Sprintf("<map[string]%s>", valueType)
		}
		if len(schemaMap(schema["properties"])) == 0 {
			return "<object (free-form)>"
		}
	}

	if valueType, ok := singleType(schema); ok {
		return fmt.Sprintf("<%s>", valueType)
	}

	return "<any>"
}

func isSingleType(schema map[string]any, want string) bool {
	valueType, ok := singleType(schema)
	return ok && valueType == want
}

func singleType(schema map[string]any) (string, bool) {
	types := normalizedTypes(schema["type"])
	if len(types) != 1 {
		return "", false
	}
	return types[0], true
}

func normalizedTypes(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed == jsonTypeNull {
			return nil
		}
		return []string{typed}
	case []any:
		return normalizedTypeArray(typed)
	default:
		return nil
	}
}

func normalizedTypeArray(values []any) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok || name == jsonTypeNull || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func isIntOrString(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if value, ok := schema["x-kubernetes-int-or-string"].(bool); ok && value {
		return true
	}
	return hasStringAndIntegerTypes(schema) || hasStringAndIntegerOneOf(schema)
}

func hasStringAndIntegerTypes(schema map[string]any) bool {
	types := normalizedTypes(schema["type"])
	if len(types) != 2 {
		return false
	}

	seen := map[string]bool{}
	for _, valueType := range types {
		seen[valueType] = true
	}
	return seen["string"] && seen["integer"]
}

func hasStringAndIntegerOneOf(schema map[string]any) bool {
	values, ok := schema["oneOf"].([]any)
	if !ok {
		return false
	}

	seen := map[string]bool{}
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			return false
		}
		if isNullType(item) {
			continue
		}
		valueType, ok := singleType(item)
		if !ok {
			return false
		}
		seen[valueType] = true
	}

	return len(seen) == 2 && seen["string"] && seen["integer"]
}

func isNullType(schema map[string]any) bool {
	value, ok := schema["type"].(string)
	return ok && value == jsonTypeNull
}

func stringifyEnumValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		if strings.Contains(typed, "|") || strings.ContainsFunc(typed, unicode.IsSpace) {
			return stringifyDefault(typed)
		}
		return typed, nil
	case json.Number:
		return typed.String(), nil
	case nil:
		return jsonTypeNull, nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	default:
		return stringifyDefault(value)
	}
}

func stringifyDefault(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))), nil
}

func cleanDescription(value string) string {
	return spaces.ReplaceAllString(newlineOrTab.ReplaceAllString(value, " "), " ")
}
