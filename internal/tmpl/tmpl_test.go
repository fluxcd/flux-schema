// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package tmpl

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestRender_FlatFormat(t *testing.T) {
	g := NewWithT(t)
	got, err := Render("{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json", SchemaVars{
		Group:   "helm.toolkit.fluxcd.io",
		Kind:    "HelmRelease",
		Version: "v2",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("helmrelease-helm-v2.json"))
}

func TestRender_NestedFormatUsesForwardSlash(t *testing.T) {
	g := NewWithT(t)
	got, err := Render("{{ .Group }}/{{ .Kind }}_{{ .Version }}.json", SchemaVars{
		Group:   "Source.Toolkit.FluxCD.io",
		Kind:    "GitRepository",
		Version: "V1",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("source.toolkit.fluxcd.io/gitrepository_v1.json"))
}

func TestRender_LowercasesAllVars(t *testing.T) {
	g := NewWithT(t)
	got, err := Render("{{ .Group }}|{{ .GroupPrefix }}|{{ .Kind }}|{{ .Version }}", SchemaVars{
		Group:       "Example.COM",
		GroupPrefix: "EXAMPLE",
		Kind:        "Widget",
		Version:     "V1BETA1",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("example.com|example|widget|v1beta1"))
}

func TestRender_DerivesGroupPrefix(t *testing.T) {
	g := NewWithT(t)
	got, err := Render("{{ .GroupPrefix }}", SchemaVars{Group: "source.toolkit.fluxcd.io"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("source"))
}

func TestRender_UnknownVarErrors(t *testing.T) {
	g := NewWithT(t)
	_, err := Render("{{ .Foo }}", SchemaVars{Kind: "Widget"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("render template"))
}

func TestRender_EmptyFormatErrors(t *testing.T) {
	g := NewWithT(t)
	_, err := Render("   ", SchemaVars{Kind: "Widget"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("empty"))
}

func TestRender_InvalidTemplateErrors(t *testing.T) {
	g := NewWithT(t)
	_, err := Render("{{ .Kind", SchemaVars{Kind: "Widget"})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("parse template"))
}
