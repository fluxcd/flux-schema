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

func docFromJSON(t *testing.T, src string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("decode document: %v", err)
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

func TestRewriteIntOrStringOneOfForCEL(t *testing.T) {
	g := NewWithT(t)
	raw := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"forward": {
				"oneOf": [
					{ "type": "string" },
					{ "type": "integer" }
				]
			},
			"reverse": {
				"description": "resource quantity",
				"oneOf": [
					{ "type": "integer" },
					{ "type": "string" }
				]
			},
			"typed": {
				"type": "string",
				"oneOf": [
					{ "type": "string" },
					{ "type": "integer" }
				]
			},
			"extraMember": {
				"oneOf": [
					{ "type": "string" },
					{ "type": "integer" },
					{ "type": "null" }
				]
			},
			"otherType": {
				"oneOf": [
					{ "type": "string" },
					{ "type": "number" }
				]
			},
			"extraConstraint": {
				"oneOf": [
					{ "type": "string", "pattern": "^[0-9]+$" },
					{ "type": "integer" }
				]
			},
			"array": {
				"type": "array",
				"items": {
					"oneOf": [
						{ "type": "string" },
						{ "type": "integer" }
					]
				}
			},
			"map": {
				"type": "object",
				"additionalProperties": {
					"oneOf": [
						{ "type": "integer" },
						{ "type": "string" }
					]
				}
			}
		}
	}`)

	got := rewriteIntOrStringOneOfForCEL(raw).(map[string]any)
	props := got["properties"].(map[string]any)
	g.Expect(props["forward"]).To(Equal(map[string]any{
		"x-kubernetes-int-or-string": true,
	}))
	g.Expect(props["reverse"]).To(Equal(map[string]any{
		"description":                "resource quantity",
		"x-kubernetes-int-or-string": true,
	}))
	g.Expect(props["array"].(map[string]any)["items"]).To(Equal(map[string]any{
		"x-kubernetes-int-or-string": true,
	}))
	g.Expect(props["map"].(map[string]any)["additionalProperties"]).To(Equal(map[string]any{
		"x-kubernetes-int-or-string": true,
	}))
	for _, name := range []string{"typed", "extraMember", "otherType", "extraConstraint"} {
		prop := props[name].(map[string]any)
		g.Expect(prop).To(HaveKey("oneOf"))
		g.Expect(prop).ToNot(HaveKey("x-kubernetes-int-or-string"))
	}

	rawProps := raw["properties"].(map[string]any)
	g.Expect(rawProps["forward"].(map[string]any)).To(HaveKey("oneOf"))
	g.Expect(rawProps["forward"].(map[string]any)).ToNot(HaveKey("x-kubernetes-int-or-string"))
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

func TestNewCELValidator_DefaultsBeforeCELAllowsUnguardedGatewayFields(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"backendRef": {
						"type": "object",
						"properties": {
							"group": { "type": "string", "default": "" },
							"kind": { "type": "string", "default": "Service" },
							"port": { "type": "integer" }
						},
						"x-kubernetes-validations": [
							{
								"rule": "(size(self.group) == 0 && self.kind == 'Service') ? has(self.port) : true",
								"message": "Must have port for Service reference"
							}
						]
					}
				}
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())
	g.Expect(v.hasDefault).To(BeTrue())

	doc := map[string]any{
		"spec": map[string]any{
			"backendRef": map[string]any{
				"port": int64(9898),
			},
		},
	}
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
}

func TestNewCELValidator_PrunesNonNullableNullsBeforeCEL(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"target": { "type": "string" }
				},
				"x-kubernetes-validations": [
					{
						"rule": "!has(self.target)",
						"message": "target must be pruned before CEL"
					}
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())
	g.Expect(v.hasDefault).To(BeFalse())

	doc := docFromJSON(t, `{
		"spec": {
			"target": null
		}
	}`)
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
	g.Expect(doc["spec"].(map[string]any)).To(HaveKey("target"))
}

func TestNewCELValidator_DefaultsBeforeCELStillReportsRuleViolation(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"backendRef": {
						"type": "object",
						"properties": {
							"group": { "type": "string", "default": "" },
							"kind": { "type": "string", "default": "Service" },
							"port": { "type": "integer" }
						},
						"x-kubernetes-validations": [
							{
								"rule": "(size(self.group) == 0 && self.kind == 'Service') ? has(self.port) : true",
								"message": "Must have port for Service reference"
							}
						]
					}
				}
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())

	doc := docFromJSON(t, `{
		"spec": {
			"backendRef": {}
		}
	}`)
	errs := v.Validate(context.Background(), doc)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Path).To(Equal("/spec/backendRef"))
	g.Expect(errs[0].Msg).To(ContainSubstring("Must have port for Service reference"))
}

func TestNewCELValidator_DefaultsArrayFieldsBeforeCEL(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"rules": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"matches": {
									"type": "array",
									"default": [
										{
											"path": {
												"type": "PathPrefix",
												"value": "/"
											}
										}
									],
									"items": {
										"type": "object",
										"properties": {
											"path": {
												"type": "object",
												"properties": {
													"type": { "type": "string" },
													"value": { "type": "string" }
												}
											}
										}
									}
								}
							}
						}
					}
				},
				"x-kubernetes-validations": [
					{
						"rule": "self.rules.all(r, r.matches.exists(m, m.path.type == 'PathPrefix' && m.path.value == '/'))",
						"message": "rule must have default path match"
					}
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())

	doc := docFromJSON(t, `{
		"spec": {
			"rules": [
				{}
			]
		}
	}`)
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
}

func TestNewCELValidator_DefaultsParentRefsListMapBeforeCEL(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"parentRefs": {
						"type": "array",
						"x-kubernetes-list-type": "map",
						"x-kubernetes-list-map-keys": ["group", "kind", "name"],
						"items": {
							"type": "object",
							"required": ["name"],
							"properties": {
								"group": { "type": "string", "default": "gateway.networking.k8s.io" },
								"kind": { "type": "string", "default": "Gateway" },
								"name": { "type": "string" },
								"sectionName": { "type": "string" }
							}
						},
						"x-kubernetes-validations": [
							{
								"rule": "self.all(p1, self.exists_one(p2, p1.group == p2.group && p1.kind == p2.kind && p1.name == p2.name))",
								"message": "sectionName must be specified when parentRefs includes 2 or more references to the same parent"
							}
						]
					}
				}
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())
	g.Expect(v.hasDefault).To(BeTrue())

	singleParent := docFromJSON(t, `{
		"spec": {
			"parentRefs": [
				{ "name": "main-gateway" }
			]
		}
	}`)
	g.Expect(v.Validate(context.Background(), singleParent)).To(BeEmpty())

	duplicateParent := docFromJSON(t, `{
		"spec": {
			"parentRefs": [
				{ "name": "main-gateway" },
				{ "name": "main-gateway" }
			]
		}
	}`)
	errs := v.Validate(context.Background(), duplicateParent)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Path).To(Equal("/spec/parentRefs"))
	g.Expect(errs[0].Msg).To(ContainSubstring("sectionName must be specified"))
}

func TestNewCELValidator_DefaultsDoNotMutateInputDocument(t *testing.T) {
	g := NewWithT(t)
	schema := schemaFromJSON(t, `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"backendRef": {
						"type": "object",
						"properties": {
							"group": { "type": "string", "default": "" },
							"kind": { "type": "string", "default": "Service" },
							"port": { "type": "integer" }
						},
						"x-kubernetes-validations": [
							{
								"rule": "(size(self.group) == 0 && self.kind == 'Service') ? has(self.port) : true",
								"message": "Must have port for Service reference"
							}
						]
					},
					"rules": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"matches": {
									"type": "array",
									"default": [
										{
											"path": {
												"type": "PathPrefix",
												"value": "/"
											}
										}
									],
									"items": {
										"type": "object",
										"properties": {
											"path": {
												"type": "object",
												"properties": {
													"type": { "type": "string" },
													"value": { "type": "string" }
												}
											}
										}
									}
								}
							}
						}
					}
				},
				"x-kubernetes-validations": [
					{
						"rule": "self.rules.all(r, r.matches.exists(m, m.path.type == 'PathPrefix' && m.path.value == '/'))",
						"message": "rule must have default path match"
					}
				]
			}
		}
	}`)
	v, err := newCELValidator(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(v).ToNot(BeNil())

	doc := map[string]any{
		"spec": map[string]any{
			"backendRef": map[string]any{
				"port": int64(9898),
			},
			"rules": []any{
				map[string]any{},
			},
		},
	}
	original := map[string]any{
		"spec": map[string]any{
			"backendRef": map[string]any{
				"port": int64(9898),
			},
			"rules": []any{
				map[string]any{},
			},
		},
	}
	g.Expect(v.Validate(context.Background(), doc)).To(BeEmpty())
	g.Expect(doc).To(Equal(original))
	spec := doc["spec"].(map[string]any)
	g.Expect(spec["backendRef"].(map[string]any)).ToNot(HaveKey("group"))
	g.Expect(spec["backendRef"].(map[string]any)).ToNot(HaveKey("kind"))
	g.Expect(spec["rules"].([]any)[0].(map[string]any)).ToNot(HaveKey("matches"))
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
