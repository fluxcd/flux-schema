// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux-schema/internal/yamldoc"
)

// ExtractRestriction reads a YAML payload and returns one Schema per restriction sample document
// found. Errors are aggregated across documents.
func ExtractRestriction(data []byte) ([]Schema, []error) {
	var out []Schema
	var errs []error
	docIndex := 0

	for _, raw := range yamldoc.Split(data) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		docIndex++
		schema, err := extractSingleRestrictionDoc(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("document %d: %w", docIndex, err))
			continue
		}
		if schema != nil {
			out = append(out, *schema)
		}
	}
	return out, errs
}

func extractSingleRestrictionDoc(raw []byte) (*Schema, error) {
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

	if doc == nil {
		return nil, nil
	}

	m, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document is not a YAML mapping")
	}

	apiVersion, _ := m[keyAPIVersion].(string)
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return nil, fmt.Errorf("missing required 'apiVersion'")
	}

	kind, _ := m[keyKind].(string)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("missing required 'kind'")
	}

	restriction, _ := m[keyRestriction].(string)
	restriction = strings.TrimSpace(restriction)
	if restriction == "" {
		return nil, fmt.Errorf("missing required 'restriction'")
	}

	group, version, ok := strings.Cut(apiVersion, "/")
	if !ok {
		group, version = "", apiVersion
	}
	if strings.Contains(version, "/") || version == "" {
		return nil, fmt.Errorf("invalid apiVersion %q", apiVersion)
	}

	scope := ""
	if meta, ok := m["metadata"].(map[string]any); ok {
		if _, hasNS := meta["namespace"]; hasNS {
			scope = "Namespaced"
		}
	}

	props := make(map[string]any, len(m))
	props[keyAPIVersion] = map[string]any{
		keyType:  typeString,
		keyConst: apiVersion,
	}
	props[keyKind] = map[string]any{
		keyType:  typeString,
		keyConst: kind,
	}

	required := []string{keyAPIVersion, keyKind}

	// Check top-level $closed and $required directives
	var closed bool
	if c, ok := m["$closed"]; ok {
		closed = isTruthy(c)
	}
	if reqVal, ok := m["$required"]; ok {
		required = append(required, parseRequiredDirective(reqVal)...)
	}

	for k, v := range m {
		if k == keyAPIVersion || k == keyKind || k == keyRestriction {
			continue
		}
		if strings.HasPrefix(k, "$") {
			if k != "$closed" && k != "$required" {
				return nil, fmt.Errorf("unknown directive %q", k)
			}
			continue
		}
		subSchema, isReq, err := inferRestrictionValue(v)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		props[k] = subSchema
		if isReq {
			required = append(required, k)
		}
	}
	slices.Sort(required)
	required = slices.Compact(required)

	jsonSchema := map[string]any{
		keyType:       typeObject,
		keyProperties: props,
		keyRequired:   toAnySlice(required),
	}
	if closed {
		jsonSchema[keyAdditionalProperties] = false
	}

	return &Schema{
		Group:       group,
		Version:     version,
		Kind:        kind,
		Restriction: restriction,
		Scope:       scope,
		JSON:        jsonSchema,
	}, nil
}

func inferRestrictionValue(v any) (any, bool, error) {
	switch n := v.(type) {
	case string:
		if n == "$forbid" {
			return false, false, nil
		}
		if strings.HasPrefix(n, "$$") {
			return map[string]any{
				keyType:  typeString,
				keyConst: n[1:],
			}, false, nil
		}
		if strings.HasPrefix(n, "$") {
			return parseRestrictionDirective(n)
		}
		return map[string]any{
			keyType:  typeString,
			keyConst: n,
		}, false, nil

	case json.Number:
		s := string(n)
		if !strings.ContainsAny(s, ".eE") {
			if i, err := n.Int64(); err == nil {
				return map[string]any{
					keyType:  typeInteger,
					keyConst: i,
				}, false, nil
			}
		}
		if f, err := n.Float64(); err == nil {
			return map[string]any{
				keyType:  typeNumber,
				keyConst: f,
			}, false, nil
		}
		return map[string]any{
			keyType:  typeNumber,
			keyConst: s,
		}, false, nil

	case int:
		return map[string]any{
			keyType:  typeInteger,
			keyConst: int64(n),
		}, false, nil

	case int32:
		return map[string]any{
			keyType:  typeInteger,
			keyConst: int64(n),
		}, false, nil

	case int64:
		return map[string]any{
			keyType:  typeInteger,
			keyConst: n,
		}, false, nil

	case float32:
		return map[string]any{
			keyType:  typeNumber,
			keyConst: float64(n),
		}, false, nil

	case float64:
		return map[string]any{
			keyType:  typeNumber,
			keyConst: n,
		}, false, nil

	case bool:
		return map[string]any{
			keyType:  typeBoolean,
			keyConst: n,
		}, false, nil

	case nil:
		return map[string]any{
			keyType: jsonNullType,
		}, false, nil

	case map[string]any:
		obj, err := inferRestrictionObject(n)
		return obj, false, err

	case []any:
		return inferRestrictionArray(n)

	default:
		return map[string]any{
			keyConst: n,
		}, false, nil
	}
}

func inferRestrictionObject(m map[string]any) (map[string]any, error) {
	props := make(map[string]any, len(m))
	var reqList []string

	var closed bool
	if c, ok := m["$closed"]; ok {
		closed = isTruthy(c)
	}
	if reqVal, ok := m["$required"]; ok {
		reqList = append(reqList, parseRequiredDirective(reqVal)...)
	}

	for k, child := range m {
		if strings.HasPrefix(k, "$") {
			if k != "$closed" && k != "$required" {
				return nil, fmt.Errorf("unknown directive %q", k)
			}
			continue
		}
		childSchema, req, err := inferRestrictionValue(child)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		props[k] = childSchema
		if req {
			reqList = append(reqList, k)
		}
	}

	out := map[string]any{
		keyType:       typeObject,
		keyProperties: props,
	}
	if closed {
		out[keyAdditionalProperties] = false
	}
	if len(reqList) > 0 {
		slices.Sort(reqList)
		reqList = slices.Compact(reqList)
		out[keyRequired] = toAnySlice(reqList)
	}
	return out, nil
}

func inferRestrictionArray(n []any) (map[string]any, bool, error) {
	if len(n) == 0 {
		return map[string]any{
			keyType: typeArray,
		}, false, nil
	}
	if len(n) == 1 {
		itemSchema, isReq, err := inferRestrictionValue(n[0])
		if err != nil {
			return nil, false, err
		}
		return map[string]any{
			keyType:  typeArray,
			keyItems: itemSchema,
		}, isReq, nil
	}

	for _, item := range n {
		if _, ok := item.(map[string]any); ok {
			return nil, false, fmt.Errorf("object arrays with more than one element are not supported")
		}
	}

	var itemType string
	hasReq := false
	for _, item := range n {
		s, req, err := inferRestrictionValue(item)
		if err != nil {
			return nil, false, err
		}
		if req {
			hasReq = true
		}
		if m, ok := s.(map[string]any); ok {
			if t, ok := m[keyType].(string); ok {
				if itemType != "" && itemType != t {
					return nil, false, fmt.Errorf("array elements must have the same type, got %q and %q", itemType, t)
				}
				itemType = t
			}
		}
	}
	res := map[string]any{
		keyType: typeArray,
	}
	if itemType != "" {
		res[keyItems] = map[string]any{keyType: itemType}
	}
	return res, hasReq, nil
}

func parseRestrictionDirective(raw string) (any, bool, error) {
	s := strings.TrimPrefix(raw, "$")

	isRequired := true
	if strings.HasSuffix(s, "?") {
		isRequired = false
		s = strings.TrimSuffix(s, "?")
	}

	// Check for $enum[...]
	if strings.HasPrefix(s, "enum[") && strings.HasSuffix(s, "]") {
		inner := s[len("enum[") : len(s)-1]
		parts := strings.Split(inner, ",")
		enumVals := make([]any, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				enumVals = append(enumVals, trimmed)
			}
		}
		if len(enumVals) == 0 {
			return nil, false, fmt.Errorf("invalid enum directive %q: empty enum", raw)
		}
		return map[string]any{
			keyType: typeString,
			keyEnum: enumVals,
		}, isRequired, nil
	}
	if strings.HasPrefix(s, "enum") {
		return nil, false, fmt.Errorf("invalid enum directive %q: expected $enum[val1, val2, ...]", raw)
	}

	// Check for $type?(...)
	if strings.Contains(s, "?(") {
		isRequired = false
		s = strings.Replace(s, "?(", "(", 1)
	}

	// Check for type with arguments: $<type>(...)
	if idx := strings.IndexByte(s, '('); idx != -1 && strings.HasSuffix(s, ")") {
		typeName := s[:idx]
		argsStr := s[idx+1 : len(s)-1]
		schema, err := parseTypeWithArgs(typeName, argsStr, raw)
		if err != nil {
			return nil, false, err
		}
		return schema, isRequired, nil
	}

	isArray := false
	if strings.HasSuffix(s, "[]") {
		isArray = true
		s = strings.TrimSuffix(s, "[]")
	}

	baseType := normalizeTypeName(s)
	if baseType == "" {
		return nil, false, fmt.Errorf("unknown directive %q", raw)
	}

	if isArray {
		return map[string]any{
			keyType:  typeArray,
			keyItems: map[string]any{keyType: baseType},
		}, isRequired, nil
	}

	return map[string]any{
		keyType: baseType,
	}, isRequired, nil
}

func parseTypeWithArgs(typeName, argsStr, rawDirective string) (map[string]any, error) {
	baseType := normalizeTypeName(typeName)
	if baseType == "" {
		return nil, fmt.Errorf("unknown directive %q: unknown type %q", rawDirective, typeName)
	}

	schema := map[string]any{
		keyType: baseType,
	}

	argsStr = strings.TrimSpace(argsStr)
	if argsStr == "" {
		return schema, nil
	}

	prefix, patternVal, hasPattern, err := extractPatternArg(baseType, typeName, argsStr, rawDirective)
	if err != nil {
		return nil, err
	}
	if hasPattern {
		schema[keyPattern] = patternVal
		argsStr = prefix
	}

	if argsStr != "" {
		pairs := strings.Split(argsStr, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("invalid argument %q in %q: expected key=value", pair, rawDirective)
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				return nil, fmt.Errorf("invalid argument %q in %q: expected key=value", pair, rawDirective)
			}
			if err := applyTypeArg(schema, baseType, typeName, k, v, rawDirective, hasPattern); err != nil {
				return nil, err
			}
		}
	}

	return schema, nil
}

func extractPatternArg(baseType, typeName, argsStr, rawDirective string) (string, string, bool, error) {
	lowerArgs := strings.ToLower(argsStr)
	pIdx := strings.Index(lowerArgs, "pattern=")
	if pIdx == -1 {
		return argsStr, "", false, nil
	}
	if baseType != typeString {
		return "", "", false, fmt.Errorf("pattern argument is not supported for type %q in %q", typeName, rawDirective)
	}
	prefix := strings.TrimSpace(argsStr[:pIdx])
	if prefix != "" {
		if !strings.HasSuffix(prefix, ",") {
			return "", "", false, fmt.Errorf("invalid arguments in %q: missing comma before pattern", rawDirective)
		}
		prefix = strings.TrimSpace(strings.TrimSuffix(prefix, ","))
	}

	patternVal := strings.TrimSpace(argsStr[pIdx+len("pattern="):])
	if patternVal == "" {
		return "", "", false, fmt.Errorf("pattern argument cannot be empty in %q", rawDirective)
	}

	// Enforce: pattern must be the last argument.
	for _, argKey := range []string{",minlength=", ", minlength=", ",maxlength=", ", maxlength=", ",format=", ", format=", ",min=", ", min=", ",max=", ", max="} {
		if strings.Contains(strings.ToLower(patternVal), argKey) {
			return "", "", false, fmt.Errorf("pattern must be the last argument in %q", rawDirective)
		}
	}

	if _, err := regexp.Compile(patternVal); err != nil {
		return "", "", false, fmt.Errorf("invalid pattern regex %q in %q: %w", patternVal, rawDirective, err)
	}

	return prefix, patternVal, true, nil
}

func applyTypeArg(schema map[string]any, baseType, typeName, k, v, rawDirective string, hasPattern bool) error {
	switch strings.ToLower(k) {
	case "pattern":
		if hasPattern {
			return fmt.Errorf("duplicate pattern argument in %q", rawDirective)
		}
		return fmt.Errorf("pattern must be the last argument in %q", rawDirective)

	case "min", "minimum":
		if baseType != typeInteger && baseType != typeNumber {
			return fmt.Errorf("argument %q not supported for type %q in %q", k, typeName, rawDirective)
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			schema[keyMinimum] = n
		} else if f, err := strconv.ParseFloat(v, 64); err == nil {
			schema[keyMinimum] = f
		} else {
			return fmt.Errorf("invalid minimum value %q in %q", v, rawDirective)
		}

	case "max", "maximum":
		if baseType != typeInteger && baseType != typeNumber {
			return fmt.Errorf("argument %q not supported for type %q in %q", k, typeName, rawDirective)
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			schema[keyMaximum] = n
		} else if f, err := strconv.ParseFloat(v, 64); err == nil {
			schema[keyMaximum] = f
		} else {
			return fmt.Errorf("invalid maximum value %q in %q", v, rawDirective)
		}

	case "minlength":
		if baseType != typeString {
			return fmt.Errorf("argument %q not supported for type %q in %q", k, typeName, rawDirective)
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid minlength value %q in %q", v, rawDirective)
		}
		schema[keyMinLength] = n

	case "maxlength":
		if baseType != typeString {
			return fmt.Errorf("argument %q not supported for type %q in %q", k, typeName, rawDirective)
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid maxlength value %q in %q", v, rawDirective)
		}
		schema[keyMaxLength] = n

	case "format":
		if baseType != typeString {
			return fmt.Errorf("argument %q not supported for type %q in %q", k, typeName, rawDirective)
		}
		schema[keyFormat] = v

	default:
		return fmt.Errorf("unknown argument %q for type %q in %q", k, typeName, rawDirective)
	}
	return nil
}

func normalizeTypeName(s string) string {
	switch strings.ToLower(s) {
	case typeString, "str":
		return typeString
	case typeInteger, "int":
		return typeInteger
	case typeNumber, "float":
		return typeNumber
	case typeBoolean, "bool":
		return typeBoolean
	case typeObject, "map":
		return typeObject
	case typeArray, "list":
		return typeArray
	default:
		return ""
	}
}

func parseRequiredDirective(val any) []string {
	var req []string
	switch r := val.(type) {
	case []any:
		for _, item := range r {
			if s, ok := item.(string); ok && s != "" {
				req = append(req, strings.TrimSpace(s))
			}
		}
	case string:
		if r != "" {
			req = append(req, strings.TrimSpace(r))
		}
	}
	return req
}

func isTruthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	default:
		return false
	}
}
