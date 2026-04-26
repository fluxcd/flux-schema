// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

// schemaFromJSON decodes a JSON literal into the map[string]any shape the
// loader hands to newCELValidator.
func schemaFromJSON(t *testing.T, src string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	return m
}

func TestNewCELValidator_NoRulesReturnsNil(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": { "type": "object" }
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).To(BeNil())
}

func TestNewCELValidator_RuleViolationProducesError(t *testing.T) {
	g := NewWithT(t)
	// Top-level rule: spec.foo must equal "ok".
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": { "foo": { "type": "string" } },
				"x-kubernetes-validations": [
					{ "rule": "self.foo == 'ok'", "message": "spec.foo must be ok" }
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())

	doc := map[string]any{"spec": map[string]any{"foo": "bad"}}
	errs := v.Validate(context.Background(), doc)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Path).To(Equal("/spec"))
	g.Expect(errs[0].Msg).To(ContainSubstring("spec.foo must be ok"))
}

func TestNewCELValidator_RulePassesWhenSatisfied(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": { "foo": { "type": "string" } },
				"x-kubernetes-validations": [
					{ "rule": "self.foo == 'ok'", "message": "spec.foo must be ok" }
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())

	doc := map[string]any{"spec": map[string]any{"foo": "ok"}}
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
}

func TestNewCELValidator_TransitionRuleSkippedWithNilOldObj(t *testing.T) {
	g := NewWithT(t)
	// Immutability rule: spec.name must equal oldSelf. With oldObj nil and
	// optionalOldSelf unset, the kube-apiserver convention is to skip the
	// transition rule entirely — no error should fire even though there is
	// no prior state to compare against.
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": { "name": { "type": "string" } },
				"x-kubernetes-validations": [
					{ "rule": "self.name == oldSelf.name", "message": "name is immutable" }
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())

	doc := map[string]any{"spec": map[string]any{"name": "anything"}}
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
}

func TestNewCELValidator_BadRuleSyntaxSurfacesPerDocument(t *testing.T) {
	g := NewWithT(t)
	// Syntactically broken CEL ("self.foo ==" is missing the right operand).
	// NewValidator does not return an error; the failure must surface from
	// Validate as a per-document violation.
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"x-kubernetes-validations": [
					{ "rule": "self.foo ==", "message": "broken" }
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())

	doc := map[string]any{"spec": map[string]any{}}
	errs := v.Validate(context.Background(), doc)
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Msg).ToNot(BeEmpty())
}

func TestNewCELValidator_ArrayTypeWithoutRulesShortCircuits(t *testing.T) {
	g := NewWithT(t)
	// `nullableOptional` produces type: ["string", "null"]; that doesn't
	// decode into apiextensionsv1.JSONSchemaProps.Type (single string).
	// The catalog's native Kubernetes schemas mix this transform but don't
	// carry x-kubernetes-validations rules, so newCELValidator must
	// short-circuit cleanly: no error, no validator, JSON Schema validation
	// continues unaffected.
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": ["string", "null"]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).To(BeNil())
}

func TestNewCELValidator_ArrayTypeWithRulesIsSoftBuildError(t *testing.T) {
	g := NewWithT(t)
	// Forward-compat guard: if a future schema source mixes nullableOptional
	// (type: ["string","null"]) with x-kubernetes-validations rules, the
	// JSONSchemaProps decode must surface as a build error rather than a
	// panic — and crucially must NOT bubble up as a schema-load failure.
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": ["string", "null"]
			}
		},
		"x-kubernetes-validations": [
			{ "rule": "true", "message": "noop" }
		]
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).To(HaveOccurred())
	g.Expect(v).To(BeNil())
}

func TestFieldPathToJSONPointer(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"spec":                    "/spec",
		"spec.kubeConfig":         "/spec/kubeConfig",
		"spec.containers[0]":      "/spec/containers/0",
		"spec.containers[0].name": "/spec/containers/0/name",
		"a/b":                     "/a~1b", // RFC 6901 escape for '/'
		"a~b":                     "/a~0b", // RFC 6901 escape for '~'
		"spec.containers[0][1].x": "/spec/containers/0/1/x",
		"spec.ports[name=http]":   "/spec/ports/http", // keyed-list selector
		"[].spec":                 "/spec",            // empty root from field.NewPath("")
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(fieldPathToJSONPointer(in)).To(Equal(want))
		})
	}
}
