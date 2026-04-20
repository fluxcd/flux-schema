// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"testing"

	. "github.com/onsi/gomega"
)

const bareCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                name:
                  type: string
`

const listCRDs = `
apiVersion: v1
kind: List
items:
  - apiVersion: apiextensions.k8s.io/v1
    kind: CustomResourceDefinition
    metadata:
      name: widgets.example.com
    spec:
      group: example.com
      names:
        kind: Widget
      versions:
        - name: v1
          schema:
            openAPIV3Schema:
              type: object
              properties:
                spec:
                  type: object
  - apiVersion: apiextensions.k8s.io/v1
    kind: CustomResourceDefinition
    metadata:
      name: gadgets.example.com
    spec:
      group: example.com
      names:
        kind: Gadget
      versions:
        - name: v1alpha1
          schema:
            openAPIV3Schema:
              type: object
`

const multiDoc = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gadgets.example.com
spec:
  group: example.com
  names:
    kind: Gadget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

const leadingSeparator = `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

const missingSchemaOnOneVersion = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1alpha1
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
`

func TestExtract_BareCRD(t *testing.T) {
	g := NewWithT(t)
	crds, errs := Extract([]byte(bareCRD))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Group).To(Equal("example.com"))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[0].Version).To(Equal("v1"))
	g.Expect(crds[0].Schema).To(HaveKey("properties"))
}

func TestExtract_List(t *testing.T) {
	g := NewWithT(t)
	crds, errs := Extract([]byte(listCRDs))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(2))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[1].Kind).To(Equal("Gadget"))
	g.Expect(crds[1].Version).To(Equal("v1alpha1"))
}

func TestExtract_MultiDocument(t *testing.T) {
	g := NewWithT(t)
	crds, errs := Extract([]byte(multiDoc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(2))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[1].Kind).To(Equal("Gadget"))
}

func TestExtract_LeadingSeparator(t *testing.T) {
	g := NewWithT(t)
	crds, errs := Extract([]byte(leadingSeparator))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
}

func TestExtract_MissingSchemaOnOneVersion(t *testing.T) {
	g := NewWithT(t)
	crds, errs := Extract([]byte(missingSchemaOnOneVersion))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring(`"v1alpha1" has no schema.openAPIV3Schema`))
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Version).To(Equal("v1"))
}

func TestExtract_NonMappingDocumentIsError(t *testing.T) {
	g := NewWithT(t)
	_, errs := Extract([]byte("- just\n- a\n- list\n"))
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Error()).To(ContainSubstring("not a YAML mapping"))
}

func TestExtract_MissingSpec(t *testing.T) {
	g := NewWithT(t)
	_, errs := Extract([]byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: x\n"))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("missing 'spec'"))
}

func TestAddAdditionalPropertiesFalse_SkipsRoot(t *testing.T) {
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
	addAdditionalPropertiesFalse(schema, true)

	_, rootHasAP := schema["additionalProperties"]
	g.Expect(rootHasAP).To(BeFalse(), "root should not have additionalProperties set")

	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)
	g.Expect(spec["additionalProperties"]).To(BeFalse())
}

func TestAddAdditionalPropertiesFalse_SkipsPreserveUnknownFields(t *testing.T) {
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
	addAdditionalPropertiesFalse(schema, true)

	values := schema["properties"].(map[string]any)["values"].(map[string]any)
	_, valuesHasAP := values["additionalProperties"]
	g.Expect(valuesHasAP).To(BeFalse(), "preserve-unknown-fields subtree must stay open")
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
