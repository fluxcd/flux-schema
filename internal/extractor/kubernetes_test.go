// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

// mustMarshal serialises v as JSON for ExtractKubernetes test inputs.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestExtractKubernetes_NoDefinitions(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractKubernetes([]byte(`{}`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("no 'definitions'"))
}

func TestExtractKubernetes_InvalidJSON(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractKubernetes([]byte(`not json`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("decode swagger"))
}

func TestExtractKubernetes_NonObjectRoot(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractKubernetes([]byte(`[]`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("not a JSON object"))
}

func TestExtractKubernetes_SkipsWatchEventAndOptions(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"io.k8s.apimachinery.pkg.apis.meta.v1.WatchEvent": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "", "version": "v1", "kind": "WatchEvent"},
					map[string]any{"group": "apps", "version": "v1", "kind": "WatchEvent"},
				},
			},
			"io.k8s.apimachinery.pkg.apis.meta.v1.DeleteOptions": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "", "version": "v1", "kind": "DeleteOptions"},
				},
			},
			"io.k8s.api.core.v1.Pod": map[string]any{
				"type": "object",
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))
	g.Expect(out[0].Kind).To(Equal("Pod"))
}

func TestExtractKubernetes_SkipsDefinitionsWithoutGVK(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"helper": map[string]any{"type": "object"},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(BeEmpty())
}

func TestExtractKubernetes_InlinesRef(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))
	widget := out[0]
	g.Expect(widget.Group).To(Equal("example.com"))
	g.Expect(widget.Version).To(Equal("v1"))
	g.Expect(widget.Kind).To(Equal("Widget"))

	props := widget.JSON["properties"].(map[string]any)
	helper := props["helper"].(map[string]any)
	g.Expect(helper["type"]).To(Equal("object"))
	g.Expect(helper).To(HaveKey("properties"))
}

func TestExtractKubernetes_CoreGroupKept(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))
	// Group left empty here; tmpl.Execute normalizes to "core".
	g.Expect(out[0].Group).To(Equal(""))
	g.Expect(out[0].Kind).To(Equal("Pod"))
}

func TestExtractKubernetes_IntOrString(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"port": map[string]any{
						"type":                       "string",
						"format":                     "int-or-string",
						"default":                    "80",
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	props := out[0].JSON["properties"].(map[string]any)
	port := props["port"].(map[string]any)
	g.Expect(port).To(HaveKey("oneOf"))
	g.Expect(port["default"]).To(Equal("80"), "metadata sibling survives the rewrite")
	g.Expect(port).ToNot(HaveKey("type"))
	g.Expect(port).ToNot(HaveKey("format"))
	g.Expect(port).ToNot(HaveKey("x-kubernetes-int-or-string"))
}

func TestExtractKubernetes_PreserveUnknownFields(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	props := out[0].JSON["properties"].(map[string]any)
	values := props["values"].(map[string]any)
	g.Expect(values["x-kubernetes-preserve-unknown-fields"]).To(BeTrue(),
		"preserve-unknown-fields extension is retained")
	_, hasAP := values["additionalProperties"]
	g.Expect(hasAP).To(BeFalse(), "preserve-unknown subtree stays open")
}

func TestExtractKubernetes_PreserveUnknownFieldsSkipsNullable(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	values := out[0].JSON["properties"].(map[string]any)["values"].(map[string]any)
	inner := values["properties"].(map[string]any)["inner"].(map[string]any)
	g.Expect(inner["type"]).To(Equal("string"),
		"properties inside a preserve-unknown subtree must not be nulled")
}

func TestExtractKubernetes_NestedNullableOptional(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	spec := out[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	name := specProps["name"].(map[string]any)
	g.Expect(name["type"]).To(Equal("string"), "required nested field stays scalar")
	optional := specProps["optional"].(map[string]any)
	g.Expect(optional["type"]).To(ConsistOf("integer", "null"), "optional nested field is nulled")
}

func TestExtractKubernetes_IndirectCycle(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	// A.properties.b.properties.a must bail out as preserve-unknown because
	// resolving it re-enters A.
	b := out[0].JSON["properties"].(map[string]any)["b"].(map[string]any)
	a := b["properties"].(map[string]any)["a"].(map[string]any)
	g.Expect(a["x-kubernetes-preserve-unknown-fields"]).To(BeTrue())
}

func TestExtractKubernetes_NullableOptional(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	name := props["name"].(map[string]any)
	g.Expect(name["type"]).To(Equal("string"), "required field stays scalar type")

	optional := props["optional"].(map[string]any)
	g.Expect(optional["type"]).To(ConsistOf("integer", "null"))
}

func TestExtractKubernetes_NullableIdempotent(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	already := props["already"].(map[string]any)
	g.Expect(already["type"]).To(ConsistOf("string", "null"))
}

func TestExtractKubernetes_OneOfGetsNullBranch(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	port := props["port"].(map[string]any)
	oneOf := port["oneOf"].([]any)
	g.Expect(oneOf).To(HaveLen(3))
	g.Expect(oneOf[2]).To(Equal(map[string]any{"type": "null"}))
}

func TestExtractKubernetes_OneOfWithExistingNullBranch(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	opt := props["opt"].(map[string]any)
	oneOf := opt["oneOf"].([]any)
	g.Expect(oneOf).To(HaveLen(2), "existing null branch prevents a duplicate")
}

func TestExtractKubernetes_ObjectWithEmptyRequired(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	a := props["a"].(map[string]any)
	g.Expect(a["type"]).To(ConsistOf("string", "null"))
}

func TestExtractKubernetes_GVKInjection(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	schema := out[0].JSON
	props := schema["properties"].(map[string]any)
	apiVersion := props["apiVersion"].(map[string]any)
	g.Expect(apiVersion["type"]).To(Equal("string"))

	kind := props["kind"].(map[string]any)
	g.Expect(kind["type"]).To(Equal("string"))

	required := schema["required"].([]any)
	g.Expect(required).To(ContainElement("apiVersion"))
	g.Expect(required).To(ContainElement("kind"))
}

func TestExtractKubernetes_GVKInjectionIdempotent(t *testing.T) {
	g := NewWithT(t)
	// Existing apiVersion / kind properties are left untouched.
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"apiVersion": map[string]any{"type": "string", "default": "custom/v1"},
					"kind":       map[string]any{"type": "string", "default": "Widget"},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	apiVersion := props["apiVersion"].(map[string]any)
	g.Expect(apiVersion["default"]).To(Equal("custom/v1"))
	kind := props["kind"].(map[string]any)
	g.Expect(kind["default"]).To(Equal("Widget"))
}

func TestExtractKubernetes_NoMetadataForBinding(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	props := out[0].JSON["properties"].(map[string]any)
	g.Expect(props).ToNot(HaveKey("metadata"))
	required := out[0].JSON["required"].([]any)
	g.Expect(required).ToNot(ContainElement("metadata"))
}

func TestExtractKubernetes_RefSiblingMetadataWins(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Helper": map[string]any{
				"type":    "object",
				"default": "shared",
			},
			"example.v1.Widget": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"helper": map[string]any{
						"$ref":    "#/definitions/example.v1.Helper",
						"default": "contextual",
					},
				},
				"required": []any{"helper"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	helper := out[0].JSON["properties"].(map[string]any)["helper"].(map[string]any)
	g.Expect(helper["default"]).To(Equal("contextual"))
}

func TestExtractKubernetes_RefSiblingTypeLoses(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	helper := out[0].JSON["properties"].(map[string]any)["helper"].(map[string]any)
	g.Expect(helper["type"]).To(Equal("object"), "structural type from the inlined definition wins")
}

func TestExtractKubernetes_RefCycleBailsOut(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	self := out[0].JSON["properties"].(map[string]any)["self"].(map[string]any)
	g.Expect(self["x-kubernetes-preserve-unknown-fields"]).To(BeTrue())
}

func TestExtractKubernetes_MultipleGVK(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(2))
}

func TestExtractKubernetes_MissingRefIsError(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	// Processing continues; a placeholder is emitted.
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(1))

	broken := out[0].JSON["properties"].(map[string]any)["broken"].(map[string]any)
	g.Expect(broken["type"]).To(Equal("object"))
	g.Expect(broken["description"]).To(ContainSubstring("unresolved $ref"))
}

func TestExtractKubernetes_StripsVendorExtensions(t *testing.T) {
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
					"count": map[string]any{
						"type": "integer",
						"x-kubernetes-validations": []any{
							map[string]any{"rule": "self >= 0", "message": "must be non-negative"},
						},
					},
				},
				"required": []any{"count"},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	g.Expect(out[0].JSON).ToNot(HaveKey("x-kubernetes-group-version-kind"))

	list := out[0].JSON["properties"].(map[string]any)["list"].(map[string]any)
	g.Expect(list).ToNot(HaveKey("x-kubernetes-list-type"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-list-map-keys"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-patch-strategy"))
	g.Expect(list).ToNot(HaveKey("x-kubernetes-patch-merge-key"))

	// CEL validations are retained so the API server's rules survive in the
	// extracted schema.
	count := out[0].JSON["properties"].(map[string]any)["count"].(map[string]any)
	g.Expect(count).To(HaveKey("x-kubernetes-validations"))
}

func TestExtractKubernetes_PreservesDescriptions(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"definitions": map[string]any{
			"example.v1.Widget": map[string]any{
				"type":        "object",
				"description": "top-level description",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "property description",
					},
				},
				"x-kubernetes-group-version-kind": []any{
					map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"},
				},
			},
		},
	}
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	g.Expect(out[0].JSON["description"]).To(Equal("top-level description"))
	name := out[0].JSON["properties"].(map[string]any)["name"].(map[string]any)
	g.Expect(name["description"]).To(Equal("property description"))
}

func TestExtractKubernetes_SortedOutput(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out).To(HaveLen(2))
	g.Expect(out[0].Group).To(Equal("a.example.com"))
	g.Expect(out[1].Group).To(Equal("b.example.com"))
}

func TestExtractKubernetes_PreservesJSONNumber(t *testing.T) {
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
	out, errs := ExtractKubernetes(raw)
	g.Expect(errs).To(BeEmpty())

	count := out[0].JSON["properties"].(map[string]any)["count"].(map[string]any)
	g.Expect(count["default"].(json.Number).String()).To(Equal("42"))
	g.Expect(count["minimum"].(json.Number).String()).To(Equal("1"))
}

func TestExtractKubernetes_RootIsClosed(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(out[0].JSON["additionalProperties"]).To(BeFalse(),
		"root must reject undocumented top-level keys")
}

func TestExtractKubernetes_DoesNotOverwriteExistingAdditionalProperties(t *testing.T) {
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
	out, errs := ExtractKubernetes(mustMarshal(t, doc))
	g.Expect(errs).To(BeEmpty())

	labels := out[0].JSON["properties"].(map[string]any)["labels"].(map[string]any)
	ap, ok := labels["additionalProperties"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "typed free-form map schema is preserved")
	g.Expect(ap["type"]).To(Equal("string"))
}
