// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	apiextschemacel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// celValidator wraps the kube-apiserver's x-kubernetes-validations evaluator
// for a single resolved schema. Construction can fail (bad JSON shape,
// unsupported structural features); evaluation cannot fail to start, but rule
// compile errors and runtime violations both surface as field.ErrorList
// entries from Validate. Validation mirrors kube-apiserver ordering by applying
// structural defaults before CEL evaluation, so rules can safely dereference
// defaulted fields as real CRDs do.
type celValidator struct {
	v          *apiextschemacel.Validator
	structural *apiextschema.Structural
	hasDefault bool
}

// newCELValidator builds a validator from a raw JSON Schema map (the same
// map[string]any decoded by the schema loader). Returns:
//   - (nil, nil) when the schema carries no x-kubernetes-validations rules.
//   - (nil, err) when the schema HAS rules but cannot be converted into a
//     structural form for CEL evaluation. Callers MUST treat err as a soft,
//     per-document surfaceable problem — not a schema-load failure — so that
//     schemas unrelated to CEL still validate normally.
//
// The "has rules?" pre-check matters: the catalog's native Kubernetes
// schemas use type: ["string","null"] (the extractor's nullableOptional
// transform) which apiextensionsv1.JSONSchemaProps cannot decode. Those
// schemas don't carry CEL rules, so we must return (nil, nil) without
// attempting the unmarshal — otherwise every Secret/ConfigMap/etc. would
// surface a spurious build error.
func newCELValidator(raw map[string]any) (*celValidator, error) {
	if raw == nil || !hasCELRules(raw) {
		return nil, nil
	}

	// The internal apiextensions.JSONSchemaProps has no JSON tags, so the
	// versioned type is the only viable unmarshal target on the way to
	// NewStructural.
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	var v1Props apiextensionsv1.JSONSchemaProps
	if err := json.Unmarshal(body, &v1Props); err != nil {
		return nil, fmt.Errorf("decode JSONSchemaProps: %w", err)
	}

	var internalProps apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&v1Props, &internalProps, nil); err != nil {
		return nil, fmt.Errorf("convert JSONSchemaProps: %w", err)
	}

	structural, err := apiextschema.NewStructural(&internalProps)
	if err != nil {
		return nil, fmt.Errorf("structural schema: %w", err)
	}

	// math.MaxUint64: flux-schema is a CI-time static validator running the
	// user's own catalog over their own manifests, so the per-call cost cap
	// (a multi-tenant kube-apiserver concern) is intentionally lifted.
	// NewValidator's second arg is isResourceRoot — true for catalog
	// schemas, which describe a full custom resource.
	v := apiextschemacel.NewValidator(structural, true, math.MaxUint64)
	if v == nil {
		return nil, nil
	}
	return &celValidator{
		v:          v,
		structural: structural,
		hasDefault: structuralHasDefaults(structural),
	}, nil
}

// Validate runs every compiled CEL rule over obj after applying the same
// structural-schema preprocessing kube-apiserver does for CREATE: prune
// non-defaultable nulls, then apply defaults. The input map is not mutated
// because validator.go reuses it after CEL. Applying structural defaults before
// CEL prevents false positives for rules such as Gateway API's, which
// dereference defaulted fields unguarded.
//
// oldObj is always nil — this is a static validator with no prior state — so
// kube-apiserver's transition-rule semantics apply: rules referencing oldSelf
// without optionalOldSelf:true are skipped, and rules with optionalOldSelf:true
// run with oldSelf unbound.
//
// Both rule-compile errors (e.g. malformed CEL syntax in a schema) and
// runtime rule violations come back through the same field.ErrorList; the
// caller cannot distinguish them, which is intentional — both are surfaced
// per-document.
func (c *celValidator) Validate(ctx context.Context, obj map[string]any) []ValidationError {
	if c == nil || c.v == nil {
		return nil
	}
	validatedObj := any(obj)
	if c.hasDefault || hasJSONNull(obj) {
		// DeepCopyJSON requires JSON-shaped input (int64, not int) and panics
		// otherwise; decodeDoc guarantees that shape via apimachinery util/json.
		validatedObj = runtime.DeepCopyJSON(obj)
		defaulting.PruneNonNullableNullsWithoutDefaults(validatedObj, c.structural)
		if c.hasDefault {
			defaulting.Default(validatedObj, c.structural)
		}
	}

	// The structural arg is documented as ignored by Validate; pass nil.
	errs, _ := c.v.Validate(ctx, field.NewPath(""), nil, validatedObj, nil, math.MaxInt64)
	if len(errs) == 0 {
		return nil
	}
	out := make([]ValidationError, 0, len(errs))
	for _, e := range errs {
		out = append(out, ValidationError{
			Path: fieldPathToJSONPointer(e.Field),
			Msg:  e.ErrorBody(),
		})
	}
	return out
}

func structuralHasDefaults(s *apiextschema.Structural) bool {
	if s == nil {
		return false
	}
	if s.Default.Object != nil {
		return true
	}
	if structuralHasDefaults(s.Items) {
		return true
	}
	for _, prop := range s.Properties {
		if structuralHasDefaults(&prop) {
			return true
		}
	}
	if s.AdditionalProperties != nil && structuralHasDefaults(s.AdditionalProperties.Structural) {
		return true
	}
	return false
}

func hasJSONNull(node any) bool {
	switch n := node.(type) {
	case nil:
		return true
	case map[string]any:
		for _, v := range n {
			if hasJSONNull(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if hasJSONNull(v) {
				return true
			}
		}
	}
	return false
}

// hasCELRules walks the raw schema tree and returns true on the first
// x-kubernetes-validations key it sees with a non-empty value. It is a
// short-circuit so callers can skip the costly JSONSchemaProps roundtrip
// for schemas with no CEL rules. We only descend through map[string]any
// and []any branches; anything else terminates the walk for that node.
func hasCELRules(node any) bool {
	switch n := node.(type) {
	case map[string]any:
		if v, ok := n["x-kubernetes-validations"]; ok {
			if arr, ok := v.([]any); ok && len(arr) > 0 {
				return true
			}
		}
		for _, v := range n {
			if hasCELRules(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if hasCELRules(v) {
				return true
			}
		}
	}
	return false
}

// fieldPathToJSONPointer converts a Kubernetes field.Path string like
// "spec.containers[0].name" or "spec.kubeConfig" into a JSON Pointer
// ("/spec/containers/0/name", "/spec/kubeConfig"). Empty input yields the
// document root pointer "" (matching the convention used elsewhere in this
// package). Each segment is RFC 6901 escaped via escapeJSONPointer; index
// brackets are flattened to plain numeric segments.
func fieldPathToJSONPointer(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	var seg strings.Builder
	flush := func() {
		if seg.Len() == 0 {
			return
		}
		out.WriteByte('/')
		out.WriteString(escapeJSONPointer(seg.String()))
		seg.Reset()
	}
	for i := 0; i < len(s); {
		switch s[i] {
		case '.':
			flush()
			i++
		case '[':
			flush()
			j := strings.IndexByte(s[i:], ']')
			if j < 0 {
				// Malformed; emit the rest verbatim as a single segment.
				seg.WriteString(s[i:])
				i = len(s)
				continue
			}
			inner := s[i+1 : i+j]
			i += j + 1
			// "[]" appears when the root path was created with field.NewPath(""):
			// the empty root key flattens away rather than emitting a stray
			// slash. Non-empty indices and keys emit a normal segment.
			if inner == "" {
				continue
			}
			// "[k=v]" is field.Path's keyed-list selector for
			// x-kubernetes-list-type: map; JSON Pointer has no direct form,
			// so use the value as the segment — it's the practical
			// identifier of the list element.
			if eq := strings.IndexByte(inner, '='); eq >= 0 {
				inner = inner[eq+1:]
			}
			out.WriteByte('/')
			out.WriteString(escapeJSONPointer(inner))
		default:
			seg.WriteByte(s[i])
			i++
		}
	}
	flush()
	return out.String()
}
