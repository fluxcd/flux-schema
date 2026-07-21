// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"encoding/json"
	"fmt"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	structurallisttype "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/listtype"
	schemaobjectmeta "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/objectmeta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// admissionValidator runs Kubernetes structural-schema admission checks that
// are expressed through x-kubernetes-* extensions and are not JSON Schema
// keywords. It mirrors kube-apiserver's custom resource strategy: embedded
// resource metadata validation, then list map/set uniqueness validation.
type admissionValidator struct {
	structural *apiextschema.Structural
	hasDefault bool
}

func newAdmissionValidatorFromStructural(structural *apiextschema.Structural) *admissionValidator {
	if structural == nil {
		return nil
	}
	return &admissionValidator{
		structural: structural,
		hasDefault: structuralHasDefaults(structural),
	}
}

func (v *admissionValidator) Validate(obj map[string]any) []ValidationError {
	if v == nil || v.structural == nil {
		return nil
	}

	validatedObj := any(obj)
	if v.hasDefault || hasJSONNull(obj) {
		// Match kube-apiserver CREATE ordering: prune non-defaultable nulls and
		// apply structural defaults before extension-backed admission checks.
		validatedObj = runtime.DeepCopyJSON(obj)
		defaulting.PruneNonNullableNullsWithoutDefaults(validatedObj, v.structural)
		if v.hasDefault {
			defaulting.Default(validatedObj, v.structural)
		}
	}

	validatedMap, ok := validatedObj.(map[string]any)
	if !ok {
		return nil
	}

	errs := schemaobjectmeta.Validate(nil, validatedMap, v.structural, false)
	errs = append(errs, structurallisttype.ValidateListSetsAndMaps(nil, v.structural, validatedMap)...)
	return fieldErrorsToValidationErrors(errs)
}

func hasAdmissionValidation(node any) bool {
	switch n := node.(type) {
	case map[string]any:
		if v, ok := n["x-kubernetes-embedded-resource"].(bool); ok && v {
			return true
		}
		if v, ok := n["x-kubernetes-list-type"].(string); ok && (v == "map" || v == "set") {
			return true
		}
		for _, v := range n {
			if hasAdmissionValidation(v) {
				return true
			}
		}
	case []any:
		for _, v := range n {
			if hasAdmissionValidation(v) {
				return true
			}
		}
	}
	return false
}

func newStructuralSchema(raw map[string]any) (*apiextschema.Structural, error) {
	structuralRaw := rewriteForKubernetesStructural(raw).(map[string]any)
	// $schema and $id are JSON Schema document metadata with no admission or
	// CEL semantics, but they round-trip into JSONSchemaProps fields that
	// NewStructural rejects. Third-party catalogs commonly set $schema, so
	// drop both from the structural copy instead of failing the build.
	delete(structuralRaw, "$schema")
	delete(structuralRaw, "$id")
	body, err := json.Marshal(structuralRaw)
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
	return structural, nil
}

func rewriteForKubernetesStructural(node any) any {
	return collapseNullableTypes(rewriteIntOrStringOneOfForCEL(node))
}

// collapseNullableTypes converts JSON Schema type arrays into the Kubernetes
// structural-schema representation. The extractor expresses optional nullable
// fields as type: ["T", "null"]; apiextensions expects type: "T" plus
// nullable: true for the structural copy used by admission and CEL validators.
func collapseNullableTypes(node any) any {
	switch n := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, v := range n {
			out[k] = collapseNullableTypes(v)
		}
		if rawType, ok := out["type"].([]any); ok {
			if t, nullable, ok := nonNullJSONSchemaType(rawType); ok {
				if t == "" {
					delete(out, "type")
				} else {
					out["type"] = t
				}
				if nullable {
					out["nullable"] = true
				}
			}
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = collapseNullableTypes(v)
		}
		return out
	default:
		return n
	}
}

func nonNullJSONSchemaType(values []any) (string, bool, bool) {
	types := make([]string, 0, len(values))
	nullable := false
	for _, value := range values {
		s, ok := value.(string)
		if !ok {
			return "", false, false
		}
		if s == "null" {
			nullable = true
			continue
		}
		types = append(types, s)
	}
	switch len(types) {
	case 0:
		return "", nullable, true
	case 1:
		return types[0], nullable, true
	default:
		return "", false, false
	}
}

func fieldErrorsToValidationErrors(errs field.ErrorList) []ValidationError {
	if len(errs) == 0 {
		return nil
	}
	out := make([]ValidationError, 0, len(errs))
	for _, err := range errs {
		out = append(out, ValidationError{
			Path: fieldPathToJSONPointer(err.Field),
			Msg:  err.ErrorBody(),
		})
	}
	return out
}
