// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

// mustMarshal serialises v as JSON for ExtractOpenAPI test inputs.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestExtractOpenAPI_NoDefinitions(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractOpenAPI([]byte(`{}`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("no 'definitions'"))
}

func TestExtractOpenAPI_InvalidJSON(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractOpenAPI([]byte(`not json`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("decode swagger"))
}

func TestExtractOpenAPI_NonObjectRoot(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractOpenAPI([]byte(`[]`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("not a JSON object"))
}

func TestExtractOpenAPI_SkipsDefinitionsWithoutGVK(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"helper": map[string]any{"type": "object"},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(BeEmpty())
}

func TestExtractOpenAPI_InlinesRef(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Helper": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"helper": map[string]any{"$ref": "#/definitions/example.v1.Helper"},
				},
				"required": []any{"helper"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))
	widget := out[0]
	g.Expect(widget.Group).To(Equal("example.com"))
	g.Expect(widget.Version).To(Equal("v1"))
	g.Expect(widget.Kind).To(Equal("Widget"))

	props := widget.Schema["properties"].(map[string]any)
	helper := props["helper"].(map[string]any)
	// Inlined: has object shape from the helper definition.
	g.Expect(helper["type"]).To(Equal("object"))
	g.Expect(helper).To(HaveKey("properties"))
}

func TestExtractOpenAPI_CoreGroupKept(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"io.k8s.api.core.v1.Pod": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))
	// Group left empty here; tmpl.Execute normalizes to "core".
	g.Expect(out[0].Group).To(Equal(""))
	g.Expect(out[0].Kind).To(Equal("Pod"))
}

func TestExtractOpenAPI_IntOrString(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"port": map[string]any{
						"type":                       "string",
						"format":                     "int-or-string",
						"description":                "a port",
						"x-kubernetes-int-or-string": true,
					},
				},
				"required": []any{"port"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	props := out[0].Schema["properties"].(map[string]any)
	port := props["port"].(map[string]any)
	g.Expect(port).To(HaveKey("oneOf"))
	g.Expect(port["description"]).To(Equal("a port"), "sibling description survives the rewrite")
	g.Expect(port).ToNot(HaveKey("type"))
	g.Expect(port).ToNot(HaveKey("format"))
	g.Expect(port).ToNot(HaveKey("x-kubernetes-int-or-string"))
}

func TestExtractOpenAPI_PreserveUnknownFields(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
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
				"required": []any{"values"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	props := out[0].Schema["properties"].(map[string]any)
	values := props["values"].(map[string]any)
	g.Expect(values["x-kubernetes-preserve-unknown-fields"]).To(BeTrue(),
		"preserve-unknown-fields extension is retained")
	_, hasAP := values["additionalProperties"]
	g.Expect(hasAP).To(BeFalse(), "preserve-unknown subtree stays open")
}

func TestExtractOpenAPI_PreserveUnknownFieldsSkipsNullable(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"values": map[string]any{
						"type":                                 "object",
						"x-kubernetes-preserve-unknown-fields": true,
						"properties": map[string]any{
							"inner": map[string]any{"type": "string"},
						},
					},
				},
				"required": []any{"values"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	values := out[0].Schema["properties"].(map[string]any)["values"].(map[string]any)
	inner := values["properties"].(map[string]any)["inner"].(map[string]any)
	g.Expect(inner["type"]).To(Equal("string"),
		"properties inside a preserve-unknown subtree must not be nulled")
}

func TestExtractOpenAPI_NestedNullableOptional(t *testing.T) {
	g := NewWithT(t)
	// Each nested object has its own required array; nulling must not
	// propagate downward.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spec": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":     map[string]any{"type": "string"},
							"optional": map[string]any{"type": "integer"},
						},
						"required": []any{"name"},
					},
				},
				"required": []any{"spec"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	spec := out[0].Schema["properties"].(map[string]any)["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	name := specProps["name"].(map[string]any)
	g.Expect(name["type"]).To(Equal("string"), "required nested field stays scalar")
	optional := specProps["optional"].(map[string]any)
	g.Expect(optional["type"]).To(ConsistOf("integer", "null"), "optional nested field is nulled")
}

func TestExtractOpenAPI_IndirectCycle(t *testing.T) {
	g := NewWithT(t)
	// A refs B, B refs A. This is the real-world shape of JSONSchemaProps /
	// JSONSchemaPropsOrArray.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.A": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"b": map[string]any{"$ref": "#/definitions/example.v1.B"},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "A"},
				},
			},
			"example.v1.B": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"$ref": "#/definitions/example.v1.A"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	// A.properties.b.properties.a must bail out as preserve-unknown because
	// resolving it re-enters A.
	b := out[0].Schema["properties"].(map[string]any)["b"].(map[string]any)
	a := b["properties"].(map[string]any)["a"].(map[string]any)
	g.Expect(a["x-kubernetes-preserve-unknown-fields"]).To(BeTrue())
}

func TestExtractOpenAPI_NullableOptional(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"optional": map[string]any{"type": "integer"},
				},
				"required": []any{"name"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	name := props["name"].(map[string]any)
	g.Expect(name["type"]).To(Equal("string"), "required field stays scalar type")

	optional := props["optional"].(map[string]any)
	g.Expect(optional["type"]).To(ConsistOf("integer", "null"))
}

func TestExtractOpenAPI_NullableIdempotent(t *testing.T) {
	g := NewWithT(t)
	// Optional field already typed as a list containing null — should not double-add.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"already": map[string]any{"type": []any{"string", "null"}},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	already := props["already"].(map[string]any)
	g.Expect(already["type"]).To(ConsistOf("string", "null"))
}

func TestExtractOpenAPI_OneOfGetsNullBranch(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"port": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "string"},
							map[string]any{"type": "integer"},
						},
					},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	port := props["port"].(map[string]any)
	oneOf := port["oneOf"].([]any)
	g.Expect(oneOf).To(HaveLen(3))
	g.Expect(oneOf[2]).To(Equal(map[string]any{"type": "null"}))
}

func TestExtractOpenAPI_OneOfWithExistingNullBranch(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"opt": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "string"},
							map[string]any{"type": "null"},
						},
					},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	opt := props["opt"].(map[string]any)
	oneOf := opt["oneOf"].([]any)
	g.Expect(oneOf).To(HaveLen(2), "existing null branch prevents a duplicate")
}

func TestExtractOpenAPI_ObjectWithEmptyRequired(t *testing.T) {
	g := NewWithT(t)
	// required: [] means every property is optional.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "string"},
				},
				"required": []any{},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	a := props["a"].(map[string]any)
	g.Expect(a["type"]).To(ConsistOf("string", "null"))
}

func TestExtractOpenAPI_GVKInjection(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	schema := out[0].Schema
	props := schema["properties"].(map[string]any)
	apiVersion := props["apiVersion"].(map[string]any)
	g.Expect(apiVersion["type"]).To(Equal("string"))
	g.Expect(apiVersion["description"]).To(ContainSubstring("APIVersion"))

	kind := props["kind"].(map[string]any)
	g.Expect(kind["type"]).To(Equal("string"))
	g.Expect(kind["description"]).To(ContainSubstring("Kind"))

	required := schema["required"].([]any)
	g.Expect(required).To(ContainElement("apiVersion"))
	g.Expect(required).To(ContainElement("kind"))
}

func TestExtractOpenAPI_GVKInjectionIdempotent(t *testing.T) {
	g := NewWithT(t)
	// Existing apiVersion / kind properties are left untouched.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"apiVersion": map[string]any{"type": "string", "description": "custom"},
					"kind":       map[string]any{"type": "string", "description": "custom"},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	apiVersion := props["apiVersion"].(map[string]any)
	g.Expect(apiVersion["description"]).To(Equal("custom"))
	kind := props["kind"].(map[string]any)
	g.Expect(kind["description"]).To(Equal("custom"))
}

func TestExtractOpenAPI_NoMetadataForBinding(t *testing.T) {
	g := NewWithT(t)
	// A kind without metadata (e.g. Binding-shaped) must not gain a metadata
	// property and must not require metadata.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Binding": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Binding"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].Schema["properties"].(map[string]any)
	g.Expect(props).ToNot(HaveKey("metadata"))
	required := out[0].Schema["required"].([]any)
	g.Expect(required).ToNot(ContainElement("metadata"))
}

func TestExtractOpenAPI_RefSiblingDescriptionWins(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Helper": map[string]any{
				"type":        "object",
				"description": "shared description",
			},
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"helper": map[string]any{
						"$ref":        "#/definitions/example.v1.Helper",
						"description": "contextual description",
					},
				},
				"required": []any{"helper"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	helper := out[0].Schema["properties"].(map[string]any)["helper"].(map[string]any)
	g.Expect(helper["description"]).To(Equal("contextual description"))
}

func TestExtractOpenAPI_RefSiblingTypeLoses(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Helper": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"helper": map[string]any{
						"$ref": "#/definitions/example.v1.Helper",
						"type": "string",
					},
				},
				"required": []any{"helper"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	helper := out[0].Schema["properties"].(map[string]any)["helper"].(map[string]any)
	g.Expect(helper["type"]).To(Equal("object"), "structural type from the inlined definition wins")
}

func TestExtractOpenAPI_RefCycleBailsOut(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Recursive": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"self": map[string]any{"$ref": "#/definitions/example.v1.Recursive"},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Recursive"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	self := out[0].Schema["properties"].(map[string]any)["self"].(map[string]any)
	g.Expect(self["x-kubernetes-preserve-unknown-fields"]).To(BeTrue())
}

func TestExtractOpenAPI_MultipleGVK(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.Legacy": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Legacy"},
					map[string]any{"group": "legacy.example.com", "version": "v1alpha1", "kind": "Legacy"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(2))
}

func TestExtractOpenAPI_MissingRefIsError(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"broken": map[string]any{"$ref": "#/definitions/example.v1.Missing"},
				},
				"required": []any{"broken"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	// Processing continues; a placeholder is emitted.
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	broken := out[0].Schema["properties"].(map[string]any)["broken"].(map[string]any)
	g.Expect(broken["description"]).To(ContainSubstring("unresolved $ref"))
}

func TestExtractOpenAPI_StripsVendorExtensions(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"list": map[string]any{
						"type":                         "array",
						"x-kubernetes-list-type":       "map",
						"x-kubernetes-list-map-keys":   []any{"name"},
						"x-kubernetes-patch-strategy":  "merge",
						"x-kubernetes-patch-merge-key": "name",
					},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	// Root: no GVK extension anymore.
	g.Expect(out[0].Schema).ToNot(HaveKey("x-kubernetes-group-version-kind"))

	list := out[0].Schema["properties"].(map[string]any)["list"].(map[string]any)
	g.Expect(list).ToNot(HaveKey("x-kubernetes-list-type"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-list-map-keys"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-patch-strategy"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-patch-merge-key"))
}

func TestExtractOpenAPI_SchemaURIInjected(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out[0].Schema["$schema"]).To(Equal("http://json-schema.org/schema#"))
}

func TestExtractOpenAPI_SortedOutput(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"zzz": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "b.example.com", "version": "v1", "kind": "Zeta"},
				},
			},
			"aaa": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "a.example.com", "version": "v1", "kind": "Alpha"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(2))
	g.Expect(out[0].Group).To(Equal("a.example.com"))
	g.Expect(out[1].Group).To(Equal("b.example.com"))
}

func TestExtractOpenAPI_PreservesJSONNumber(t *testing.T) {
	g := NewWithT(t)
	// Use raw JSON so the integer literal stays exact through UseNumber.
	raw := []byte(`{
  "definitions": {
    "example.v1.Widget": {
      "type": "object",
      "properties": {
        "count": {"type": "integer", "default": 42, "minimum": 1}
      },
      "required": ["count"],
      "x-kubernetes-group-version-kind": [
        {"group": "example.com", "version": "v1", "kind": "Widget"}
      ]
    }
  }
}`)
	out, errs := ExtractOpenAPI(raw)
	g.Expect(errs).To(BeEmpty())

	count := out[0].Schema["properties"].(map[string]any)["count"].(map[string]any)
	// json.Number stringifies as the original literal.
	g.Expect(count["default"].(json.Number).String()).To(Equal("42"))
	g.Expect(count["minimum"].(json.Number).String()).To(Equal("1"))
}

func TestExtractOpenAPI_RootIsClosed(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spec": map[string]any{"type": "object"},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out[0].Schema["additionalProperties"]).To(BeFalse(),
		"root must reject undocumented top-level keys")
}

func TestExtractOpenAPI_DoesNotOverwriteExistingAdditionalProperties(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"labels": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
				"required": []any{"labels"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractOpenAPI(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	labels := out[0].Schema["properties"].(map[string]any)["labels"].(map[string]any)
	ap, ok := labels["additionalProperties"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "typed free-form map schema is preserved")
	g.Expect(ap["type"]).To(Equal("string"))
}
