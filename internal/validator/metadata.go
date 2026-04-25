// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/validate/content"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
)

var regexHintRE = regexp.MustCompile(`regex used for validation is '([^']*)'`)

// shortenRule collapses an apimachinery validation message to just
// "must match regex '<pattern>'" when the message carries the regex hint.
// The full prose tail bloats CLI output without telling the user anything
// the pattern itself doesn't.
func shortenRule(s string) string {
	m := regexHintRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return s
	}
	return "must match regex '" + m[1] + "'"
}

// validateMetadata applies the kube-apiserver ObjectMeta checks to every
// document regardless of kind. Both extracted CRD schemas and the native
// K8s schemas leave metadata effectively unconstrained, so a manifest
// that passes JSON Schema validation can still be rejected by the API
// server at reconcile time.
//
// Name validation deliberately uses NameIsDNSSubdomain (the most
// permissive of the kube name validators) because we don't know each
// kind's nameFn here; tighter rules like NameIsDNSLabel (Service / Pod /
// Namespace / ConfigMap / Secret) would produce false positives across
// kinds.
func validateMetadata(doc map[string]any) []ValidationError {
	metadata, _ := doc["metadata"].(map[string]any)
	if metadata == nil {
		return nil
	}
	var out []ValidationError

	if name, ok := metadata["name"].(string); ok && name != "" {
		for _, msg := range apivalidation.NameIsDNSSubdomain(name, false) {
			out = append(out, ValidationError{Path: "/metadata/name", Msg: shortenRule(msg)})
		}
	}
	if gen, ok := metadata["generateName"].(string); ok && gen != "" {
		for _, msg := range apivalidation.NameIsDNSSubdomain(gen, true) {
			out = append(out, ValidationError{Path: "/metadata/generateName", Msg: shortenRule(msg)})
		}
	}
	if ns, ok := metadata["namespace"].(string); ok && ns != "" {
		for _, msg := range apivalidation.ValidateNamespaceName(ns, false) {
			out = append(out, ValidationError{Path: "/metadata/namespace", Msg: shortenRule(msg)})
		}
	}
	if labels, ok := metadata["labels"].(map[string]any); ok {
		out = append(out, validateLabels(labels)...)
	}
	if annotations, ok := metadata["annotations"].(map[string]any); ok {
		out = append(out, validateAnnotations(annotations)...)
	}
	return out
}

func validateLabels(labels map[string]any) []ValidationError {
	var out []ValidationError
	for k, raw := range labels {
		path := jsonPointer("metadata", "labels", k)
		v, ok := raw.(string)
		if !ok {
			out = append(out, ValidationError{
				Path: path,
				Msg:  fmt.Sprintf("must be a string, got %s", yamlTypeName(raw)),
			})
			continue
		}
		for _, msg := range content.IsLabelKey(k) {
			out = append(out, ValidationError{Path: path, Msg: "key: " + shortenRule(msg)})
		}
		for _, msg := range content.IsLabelValue(v) {
			out = append(out, ValidationError{Path: path, Msg: shortenRule(msg)})
		}
	}
	return out
}

func validateAnnotations(annotations map[string]any) []ValidationError {
	var out []ValidationError
	var totalSize int64
	for k, raw := range annotations {
		path := jsonPointer("metadata", "annotations", k)
		v, ok := raw.(string)
		if !ok {
			out = append(out, ValidationError{
				Path: path,
				Msg:  fmt.Sprintf("must be a string, got %s", yamlTypeName(raw)),
			})
			continue
		}
		// Annotation keys are case-insensitive for the qualified-name
		// check; mirror what apivalidation.ValidateAnnotations does.
		for _, msg := range content.IsLabelKey(strings.ToLower(k)) {
			out = append(out, ValidationError{Path: path, Msg: "key: " + shortenRule(msg)})
		}
		totalSize += int64(len(k)) + int64(len(v))
	}
	if totalSize > int64(apivalidation.TotalAnnotationSizeLimitB) {
		out = append(out, ValidationError{
			Path: "/metadata/annotations",
			Msg:  fmt.Sprintf("total size must be at most %d bytes", apivalidation.TotalAnnotationSizeLimitB),
		})
	}
	return out
}

func jsonPointer(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteByte('/')
		b.WriteString(escapeJSONPointer(p))
	}
	return b.String()
}

// yamlTypeName labels a value with its YAML kind so error messages render
// "got mapping" rather than leaking Go types like "map[string]interface {}".
// sigs.k8s.io/yaml decodes numbers as float64 in practice, but the
// integer cases are kept defensively for callers that might pre-stuff a doc.
func yamlTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case map[string]any:
		return "mapping"
	case []any:
		return "sequence"
	case int, int32, int64, uint, uint32, uint64, float32, float64:
		return "number"
	default:
		return "unknown"
	}
}

func escapeJSONPointer(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	// RFC 6901: encode '~' as '~0' before '/' as '~1' so '~1' inputs are
	// not double-escaped on the second pass.
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}
