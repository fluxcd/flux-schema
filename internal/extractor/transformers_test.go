// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestCloseAdditionalPropertiesChildren_LeavesRootOpen(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
	}
	closeAdditionalPropertiesChildren(schema)

	_, rootHasAP := schema["additionalProperties"]
	g.Expect(rootHasAP).To(BeFalse(), "root should not have additionalProperties set")

	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)
	g.Expect(spec["additionalProperties"]).To(BeFalse())
}

func TestCloseAdditionalPropertiesChildren_SkipsPreserveUnknownFields(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"values": map[string]any{
				"type":                                 "object",
				"x-kubernetes-preserve-unknown-fields": true,
				"properties": map[string]any{
					"nested": map[string]any{"type": "string"},
				},
			},
		},
	}
	closeAdditionalPropertiesChildren(schema)

	values := schema["properties"].(map[string]any)["values"].(map[string]any)
	_, valuesHasAP := values["additionalProperties"]
	g.Expect(valuesHasAP).To(BeFalse(), "preserve-unknown-fields subtree must stay open")
}

func TestCloseAdditionalProperties_ClosesRoot(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{"type": "object"},
		},
	}
	closeAdditionalProperties(schema)
	g.Expect(schema["additionalProperties"]).To(BeFalse(), "root must be closed")
}

func TestCloseAdditionalProperties_PreservesFieldNamedProperties(t *testing.T) {
	tests := []struct {
		name          string
		transform     func(any)
		wantRootClose bool
	}{
		{
			name:          "close root",
			transform:     closeAdditionalProperties,
			wantRootClose: true,
		},
		{
			name:      "close children only",
			transform: closeAdditionalPropertiesChildren,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			schema := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"conditions": map[string]any{"type": "array"},
							"properties": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name":  map[string]any{"type": "string"},
										"value": map[string]any{"type": "string"},
									},
								},
							},
							"version": map[string]any{"type": "string"},
						},
					},
				},
			}

			tt.transform(schema)

			_, rootHasAP := schema["additionalProperties"]
			g.Expect(rootHasAP).To(Equal(tt.wantRootClose))

			status := schema["properties"].(map[string]any)["status"].(map[string]any)
			g.Expect(status["additionalProperties"]).To(BeFalse(), "real object schema must be closed")

			statusProps := status["properties"].(map[string]any)
			g.Expect(statusProps).To(HaveLen(3), "properties map must contain only real field names")
			g.Expect(statusProps).To(HaveKey("conditions"))
			g.Expect(statusProps).To(HaveKey("properties"))
			g.Expect(statusProps).To(HaveKey("version"))
			g.Expect(statusProps).ToNot(HaveKey("additionalProperties"))

			propertiesField := statusProps["properties"].(map[string]any)
			items := propertiesField["items"].(map[string]any)
			g.Expect(items["additionalProperties"]).To(BeFalse(), "nested object schema must still be closed")
			itemProps := items["properties"].(map[string]any)
			g.Expect(itemProps).To(HaveLen(2), "nested properties map must contain only real field names")
			g.Expect(itemProps).To(HaveKey("name"))
			g.Expect(itemProps).To(HaveKey("value"))
			g.Expect(itemProps).ToNot(HaveKey("additionalProperties"))
		})
	}
}

func TestCloseAdditionalProperties_PreservesValueKeywords(t *testing.T) {
	wantDefault := map[string]any{
		"properties": float64(1),
		"nested": map[string]any{
			"properties": map[string]any{
				"name": "default",
			},
		},
	}
	wantEnum := []any{
		map[string]any{
			"properties": float64(1),
			"nested": map[string]any{
				"properties": map[string]any{
					"name": "enum-a",
				},
			},
		},
		map[string]any{
			"properties": float64(2),
			"nested": map[string]any{
				"properties": map[string]any{
					"name": "enum-b",
				},
			},
		},
	}
	wantExample := map[string]any{
		"properties": float64(3),
		"nested": map[string]any{
			"properties": map[string]any{
				"name": "example",
			},
		},
	}

	tests := []struct {
		name          string
		transform     func(any)
		wantRootClose bool
	}{
		{
			name:          "close root",
			transform:     closeAdditionalProperties,
			wantRootClose: true,
		},
		{
			name:      "close children only",
			transform: closeAdditionalPropertiesChildren,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			schema := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spec": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"cfg": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"properties": map[string]any{"type": "integer"},
									"nested": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name": map[string]any{"type": "string"},
										},
									},
								},
								"default": map[string]any{
									"properties": float64(1),
									"nested": map[string]any{
										"properties": map[string]any{
											"name": "default",
										},
									},
								},
								"enum": []any{
									map[string]any{
										"properties": float64(1),
										"nested": map[string]any{
											"properties": map[string]any{
												"name": "enum-a",
											},
										},
									},
									map[string]any{
										"properties": float64(2),
										"nested": map[string]any{
											"properties": map[string]any{
												"name": "enum-b",
											},
										},
									},
								},
								"example": map[string]any{
									"properties": float64(3),
									"nested": map[string]any{
										"properties": map[string]any{
											"name": "example",
										},
									},
								},
							},
						},
					},
				},
			}

			tt.transform(schema)

			_, rootHasAP := schema["additionalProperties"]
			g.Expect(rootHasAP).To(Equal(tt.wantRootClose))

			spec := schema["properties"].(map[string]any)["spec"].(map[string]any)
			g.Expect(spec["additionalProperties"]).To(BeFalse(), "real object schema must be closed")

			cfg := spec["properties"].(map[string]any)["cfg"].(map[string]any)
			g.Expect(cfg["additionalProperties"]).To(BeFalse(), "real object schema with value keywords must be closed")

			nestedSchema := cfg["properties"].(map[string]any)["nested"].(map[string]any)
			g.Expect(nestedSchema["additionalProperties"]).To(BeFalse(), "nested real object schema must be closed")

			g.Expect(cfg["default"]).To(Equal(wantDefault), "default value must stay untouched")
			g.Expect(cfg["enum"]).To(Equal(wantEnum), "enum values must stay untouched")
			g.Expect(cfg["example"]).To(Equal(wantExample), "example value must stay untouched")
		})
	}
}

// A structural object that also carries oneOf/anyOf branches (Cilium's
// CiliumNetworkPolicy spec pattern): the object itself is closed, but the
// branch subschemas — which list only a subset of properties to anchor a
// requirement — must stay open, else a real resource carrying sibling
// properties matches no branch and is wrongly rejected.
func TestCloseAdditionalProperties_LeavesCombinatorBranchesOpen(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"endpointSelector": map[string]any{
				"type":       "object",
				"properties": map[string]any{"matchLabels": map[string]any{"type": "object"}},
			},
			"ingress":      map[string]any{"type": "array"},
			"nodeSelector": map[string]any{"type": "object"},
		},
		"oneOf": []any{
			map[string]any{
				"properties": map[string]any{"endpointSelector": map[string]any{}},
				"required":   []any{"endpointSelector"},
			},
			map[string]any{
				"properties": map[string]any{"nodeSelector": map[string]any{}},
				"required":   []any{"nodeSelector"},
			},
		},
		"anyOf": []any{
			map[string]any{"required": []any{"ingress"}},
		},
		"allOf": []any{
			map[string]any{
				"properties": map[string]any{"ingress": map[string]any{}},
			},
		},
		"not": map[string]any{
			"properties": map[string]any{"ingress": map[string]any{}},
			"required":   []any{"ingress", "nodeSelector"},
		},
	}
	closeAdditionalProperties(schema)

	g.Expect(schema["additionalProperties"]).To(BeFalse(), "structural object must be closed")

	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		for i, branch := range schema[key].([]any) {
			_, hasAP := branch.(map[string]any)["additionalProperties"]
			g.Expect(hasAP).To(BeFalse(), "%s branch %d must stay open", key, i)
		}
	}
	_, notHasAP := schema["not"].(map[string]any)["additionalProperties"]
	g.Expect(notHasAP).To(BeFalse(), "not subschema must stay open")
	// The structural properties themselves are still closed.
	sel := schema["properties"].(map[string]any)["endpointSelector"].(map[string]any)
	g.Expect(sel["additionalProperties"]).To(BeFalse(), "nested structural object must be closed")
}

// Combinators at the openAPIV3Schema root (e.g. oneOf requiring spec or
// specs) must stay open on the CRD pipeline's root pass too, not only when
// reached through recursion.
func TestCloseAdditionalPropertiesChildren_LeavesRootCombinatorBranchesOpen(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
			"specs": map[string]any{"type": "array"},
		},
		"oneOf": []any{
			map[string]any{
				"properties": map[string]any{"spec": map[string]any{}},
				"required":   []any{"spec"},
			},
			map[string]any{
				"properties": map[string]any{"specs": map[string]any{}},
				"required":   []any{"specs"},
			},
		},
	}
	closeAdditionalPropertiesChildren(schema)

	for i, branch := range schema["oneOf"].([]any) {
		_, hasAP := branch.(map[string]any)["additionalProperties"]
		g.Expect(hasAP).To(BeFalse(), "root oneOf branch %d must stay open", i)
	}
	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)
	g.Expect(spec["additionalProperties"]).To(BeFalse(), "structural child must be closed")
}

func TestReplaceIntOrString_LegacyFormat(t *testing.T) {
	g := NewWithT(t)
	input := map[string]any{
		"properties": map[string]any{
			"port": map[string]any{"format": "int-or-string"},
		},
	}
	out := replaceIntOrString(input).(map[string]any)
	port := out["properties"].(map[string]any)["port"].(map[string]any)
	g.Expect(port).To(HaveKey("oneOf"))
}

func TestReplaceIntOrString_StructuralExtension(t *testing.T) {
	g := NewWithT(t)
	input := map[string]any{
		"properties": map[string]any{
			"port": map[string]any{"x-kubernetes-int-or-string": true},
		},
	}
	out := replaceIntOrString(input).(map[string]any)
	port := out["properties"].(map[string]any)["port"].(map[string]any)
	g.Expect(port).To(HaveKey("oneOf"))
	_, hasExt := port["x-kubernetes-int-or-string"]
	g.Expect(hasExt).To(BeFalse(), "replacement should not retain the extension")
}

func TestStripDescriptions(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type":        "object",
		"description": "root",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "prop",
			},
			"items": []any{
				map[string]any{"description": "inside-array"},
			},
		},
	}
	StripDescriptions(schema)
	g.Expect(schema).ToNot(HaveKey("description"))
	props := schema["properties"].(map[string]any)
	g.Expect(props["name"]).ToNot(HaveKey("description"))
	arr := props["items"].([]any)
	g.Expect(arr[0]).ToNot(HaveKey("description"))
}

// TestStripDescriptions_PreservesPropertyNamedDescription guards against a
// regression where a property literally named "description" (as
// JSONSchemaProps has for CRD openAPIV3Schema) was deleted along with the
// schema-metadata "description" keyword. The property itself must survive;
// only its metadata siblings underneath get stripped.
func TestStripDescriptions_PreservesPropertyNamedDescription(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type":        "object",
		"description": "root doc",
		"properties": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "the description field's own doc",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "the type field's own doc",
			},
		},
		"patternProperties": map[string]any{
			"^x-": map[string]any{
				"description": "pattern prop doc",
			},
		},
		"$defs": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "def doc",
			},
		},
		"definitions": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "legacy def doc",
			},
		},
	}
	StripDescriptions(schema)

	g.Expect(schema).ToNot(HaveKey("description"))

	props := schema["properties"].(map[string]any)
	g.Expect(props).To(HaveKey("description"), "property named 'description' must be preserved")
	g.Expect(props["description"]).ToNot(HaveKey("description"))
	g.Expect(props["description"]).To(HaveKeyWithValue("type", "string"))
	g.Expect(props).To(HaveKey("type"))

	pp := schema["patternProperties"].(map[string]any)
	g.Expect(pp).To(HaveKey("^x-"))
	g.Expect(pp["^x-"]).ToNot(HaveKey("description"))

	defs := schema["$defs"].(map[string]any)
	g.Expect(defs).To(HaveKey("description"))
	g.Expect(defs["description"]).ToNot(HaveKey("description"))

	legacyDefs := schema["definitions"].(map[string]any)
	g.Expect(legacyDefs).To(HaveKey("description"))
	g.Expect(legacyDefs["description"]).ToNot(HaveKey("description"))
}

// TestStripDescriptions_DeepNesting guards that a property named "description"
// is preserved at every level of recursion, not just the top. Mirrors the
// JSONSchemaProps shape where openAPIV3Schema → properties.foo → properties
// reaches arbitrary depth.
func TestStripDescriptions_DeepNesting(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"description": "L0",
		"properties": map[string]any{
			"description": map[string]any{
				"description": "L1",
				"properties": map[string]any{
					"description": map[string]any{
						"description": "L2",
						"type":        "string",
					},
				},
			},
		},
	}
	StripDescriptions(schema)

	l1Props := schema["properties"].(map[string]any)
	g.Expect(l1Props).To(HaveKey("description"))
	l1 := l1Props["description"].(map[string]any)
	g.Expect(l1).ToNot(HaveKey("description"))

	l2Props := l1["properties"].(map[string]any)
	g.Expect(l2Props).To(HaveKey("description"))
	l2 := l2Props["description"].(map[string]any)
	g.Expect(l2).ToNot(HaveKey("description"))
	g.Expect(l2).To(HaveKeyWithValue("type", "string"))
}

// TestStripDescriptions_SchemaValuedKeywords confirms that schema-valued
// keywords (additionalProperties, not, if/then/else, oneOf/anyOf/allOf) still
// have their "description" metadata stripped — they are schema nodes, not
// name→schema maps. Mixed with a nested properties map so both paths execute.
func TestStripDescriptions_SchemaValuedKeywords(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"additionalProperties": map[string]any{
			"description": "addl",
			"type":        "object",
			"properties": map[string]any{
				"description": map[string]any{
					"description": "nested",
					"type":        "string",
				},
			},
		},
		"not": map[string]any{"description": "not-doc", "type": "null"},
		"if":  map[string]any{"description": "if-doc"},
		"then": map[string]any{
			"description": "then-doc",
			"properties": map[string]any{
				"description": map[string]any{"description": "then-prop", "type": "string"},
			},
		},
		"else":  map[string]any{"description": "else-doc"},
		"oneOf": []any{map[string]any{"description": "oneOf-0"}},
		"anyOf": []any{map[string]any{"description": "anyOf-0"}},
		"allOf": []any{map[string]any{"description": "allOf-0"}},
	}
	StripDescriptions(schema)

	addl := schema["additionalProperties"].(map[string]any)
	g.Expect(addl).ToNot(HaveKey("description"))
	addlProps := addl["properties"].(map[string]any)
	g.Expect(addlProps).To(HaveKey("description"))
	g.Expect(addlProps["description"]).ToNot(HaveKey("description"))

	for _, k := range []string{"not", "if", "then", "else"} {
		sub, ok := schema[k].(map[string]any)
		g.Expect(ok).To(BeTrue(), "key %s should be a map", k)
		g.Expect(sub).ToNot(HaveKey("description"), "metadata under %s must be stripped", k)
	}
	thenProps := schema["then"].(map[string]any)["properties"].(map[string]any)
	g.Expect(thenProps).To(HaveKey("description"))
	g.Expect(thenProps["description"]).ToNot(HaveKey("description"))

	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		arr := schema[k].([]any)
		g.Expect(arr[0]).ToNot(HaveKey("description"), "metadata under %s[0] must be stripped", k)
	}
}

// TestStripDescriptions_ItemsWithNestedProperties pins the array `items`
// path: items is a schema node (not a name map), so its own description is
// stripped, but a property named "description" living under items.properties
// must still survive. Models `type: array` children of JSONSchemaProps.
func TestStripDescriptions_ItemsWithNestedProperties(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"description": "items-doc",
			"type":        "object",
			"properties": map[string]any{
				"description": map[string]any{
					"description": "nested under items",
					"type":        "string",
				},
			},
		},
	}
	StripDescriptions(schema)

	items := schema["items"].(map[string]any)
	g.Expect(items).ToNot(HaveKey("description"))
	itemsProps := items["properties"].(map[string]any)
	g.Expect(itemsProps).To(HaveKey("description"))
	g.Expect(itemsProps["description"]).ToNot(HaveKey("description"))
	g.Expect(itemsProps["description"]).To(HaveKeyWithValue("type", "string"))
}
