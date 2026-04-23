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
