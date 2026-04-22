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

func TestExtractCRDs_BareCRD(t *testing.T) {
	g := NewWithT(t)
	crds, errs := ExtractCRDs([]byte(bareCRD))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Group).To(Equal("example.com"))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[0].Version).To(Equal("v1"))
	g.Expect(crds[0].JSON).To(HaveKey("properties"))
}

func TestExtractCRDs_List(t *testing.T) {
	g := NewWithT(t)
	crds, errs := ExtractCRDs([]byte(listCRDs))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(2))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[1].Kind).To(Equal("Gadget"))
	g.Expect(crds[1].Version).To(Equal("v1alpha1"))
}

func TestExtractCRDs_MultiDocument(t *testing.T) {
	g := NewWithT(t)
	crds, errs := ExtractCRDs([]byte(multiDoc))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(2))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
	g.Expect(crds[1].Kind).To(Equal("Gadget"))
}

func TestExtractCRDs_LeadingSeparator(t *testing.T) {
	g := NewWithT(t)
	crds, errs := ExtractCRDs([]byte(leadingSeparator))
	g.Expect(errs).To(BeEmpty())
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Kind).To(Equal("Widget"))
}

func TestExtractCRDs_MissingSchemaOnOneVersion(t *testing.T) {
	g := NewWithT(t)
	crds, errs := ExtractCRDs([]byte(missingSchemaOnOneVersion))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring(`"v1alpha1" has no schema.openAPIV3Schema`))
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].Version).To(Equal("v1"))
}

func TestExtractCRDs_NonMappingDocumentIsError(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractCRDs([]byte("- just\n- a\n- list\n"))
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Error()).To(ContainSubstring("not a YAML mapping"))
}

func TestExtractCRDs_MissingSpec(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractCRDs([]byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: x\n"))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("missing 'spec'"))
}
