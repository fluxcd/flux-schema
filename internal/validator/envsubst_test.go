// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestEnvsubstSources(t *testing.T) {
	for _, source := range []string{
		"testdata/envsubst/deployment.yaml",
		"testdata/envsubst",
		StdinSource,
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", source, enabled), func(t *testing.T) {
				g := NewWithT(t)
				raw, err := os.ReadFile("testdata/envsubst/deployment.yaml")
				g.Expect(err).NotTo(HaveOccurred())
				opts := Options{
					SchemaLocations: []string{"../../catalog/latest/" + DefaultSchemaLayout},
					Stdin:           strings.NewReader(string(raw)),
				}
				if enabled {
					opts.Envsubst = map[string]string{"REPLICAS": "2"}
				}
				v, err := New(opts)
				g.Expect(err).NotTo(HaveOccurred())
				var results []Result
				for r := range v.ValidateSources(context.Background(), []string{source}) {
					if !r.Final {
						results = append(results, r)
					}
				}
				g.Expect(results).To(HaveLen(1))
				r := results[0]
				g.Expect(r.Source).To(Equal(source))
				g.Expect(r.DocIndex).To(Equal(1))
				g.Expect(r.Identifier()).To(Equal("apps/v1/Deployment/apps/web"))
				if source == "testdata/envsubst" {
					g.Expect(filepath.ToSlash(r.Origin)).To(Equal("testdata/envsubst/deployment.yaml"))
				}
				if enabled {
					g.Expect(r.Status).To(Equal(StatusValid))
					g.Expect(r.Errors).To(BeEmpty())
				} else {
					g.Expect(r.Reason).To(Equal(ReasonSchemaViolation))
					g.Expect(r.Errors).To(ContainElement(ValidationError{
						Path: "/spec/replicas", Msg: "got string, want null or integer",
					}))
				}
			})
		}
	}
}

func TestEnvsubstDocuments(t *testing.T) {
	for _, tt := range []struct {
		name       string
		expression string
		vars       map[string]string
		metadata   string
		want       string
		reason     Reason
		strict     bool
	}{
		{name: "value", expression: "${VAR}", vars: map[string]string{"VAR": "value"}, want: "value"},
		{name: "default with empty map", expression: "${VAR:=default}", vars: map[string]string{}, want: "default"},
		{name: "default with empty value", expression: "${VAR:=default}", vars: map[string]string{"VAR": ""}, want: "default"},
		{name: "undefined ignores process environment", expression: "prefix-${VAR}", vars: map[string]string{}, want: "prefix-"},
		{name: "nil disables substitution", expression: "${VAR}", want: "${VAR}"},
		{name: "nil preserves malformed expression", expression: "${", want: "${"},
		{name: "escaped variable", expression: "$${VAR}", vars: map[string]string{"VAR": "value"}, want: "${VAR}"},
		{name: "label disables substitution", expression: "${VAR}", vars: map[string]string{"VAR": "value"},
			metadata: "  labels:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", want: "${VAR}"},
		{name: "annotation disables substitution", expression: "${VAR}", vars: map[string]string{"VAR": "value"},
			metadata: "  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", want: "${VAR}"},
		{name: "opt-out is exact", expression: "${VAR}", vars: map[string]string{"VAR": "value"},
			metadata: "  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: enabled\n", want: "value"},
		{name: "disabled document is still validated", expression: "${VAR}", vars: map[string]string{"VAR": "value"},
			metadata: "  labels:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", want: "value", reason: ReasonSchemaViolation},
		{name: "strict defined", expression: "${VAR}", vars: map[string]string{"VAR": "value"}, strict: true, want: "value"},
		{name: "strict empty value", expression: "prefix-${VAR}", vars: map[string]string{"VAR": ""}, strict: true, want: "prefix-"},
		{name: "strict default", expression: "${VAR:=default}", vars: map[string]string{}, strict: true, want: "default"},
		{name: "strict undefined", expression: "${VAR}", vars: map[string]string{}, strict: true, reason: ReasonEnvsubstError},
		{name: "strict disabled", expression: "${VAR}", vars: map[string]string{}, strict: true,
			metadata: "  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", want: "${VAR}"},
		{name: "newlines stripped from values", expression: "${VAR}", vars: map[string]string{"VAR": "l1\nl2"}, want: "l1l2"},
		{name: "quoted variable takes value type", expression: "${VAR}", vars: map[string]string{"VAR": "2"}, reason: ReasonSchemaViolation},
		{name: "malformed expression", expression: "${", vars: map[string]string{}, reason: ReasonEnvsubstError},
		{name: "disabled malformed expression", expression: "${", vars: map[string]string{},
			metadata: "  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", want: "${"},
		{name: "duplicate key remains invalid", expression: "${VAR}", vars: map[string]string{"VAR": "value"},
			metadata: "  name: duplicate\n", reason: ReasonYAMLParseError},
		{name: "disabled duplicate key remains invalid", expression: "${", vars: map[string]string{},
			metadata: "  labels:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n  name: duplicate\n", reason: ReasonYAMLParseError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv("VAR", "must-not-be-used")
			schemaDir := t.TempDir()
			want, err := json.Marshal(tt.want)
			g.Expect(err).NotTo(HaveOccurred())
			schema := fmt.Sprintf(`{"type":"object","properties":{"spec":{"type":"object","properties":{"name":{"type":"string","const":%s}}}}}`, want)
			g.Expect(os.WriteFile(filepath.Join(schemaDir, "widget.json"), []byte(schema), 0o644)).To(Succeed())
			v, err := New(Options{SchemaLocations: []string{schemaDir + "/{{.Kind}}.json"}, Envsubst: tt.vars, EnvsubstStrict: tt.strict})
			g.Expect(err).NotTo(HaveOccurred())
			raw := fmt.Sprintf("apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: web\n  namespace: apps\n%sspec:\n  name: '%s'\n", tt.metadata, tt.expression)
			results := v.ValidateBytes(context.Background(), "input.yaml", []byte(raw))
			g.Expect(results).To(HaveLen(1))
			r := results[0]
			g.Expect(r.Reason).To(Equal(tt.reason))
			if tt.reason == ReasonNone {
				g.Expect(r.Status).To(Equal(StatusValid))
				g.Expect(r.Errors).To(BeEmpty())
			} else {
				g.Expect(r.Status).To(Equal(StatusInvalid))
				g.Expect(r.Errors).NotTo(BeEmpty())
			}
			if tt.reason == ReasonEnvsubstError {
				g.Expect(r.Identifier()).To(Equal("example.com/v1/Widget/apps/web"))
				g.Expect(r.Errors).To(HaveLen(1))
				g.Expect(r.Errors[0].Path).To(BeEmpty())
				g.Expect(r.Errors[0].Msg).NotTo(BeEmpty())
				if tt.strict {
					g.Expect(r.Errors[0].Msg).To(ContainSubstring(`variable not set (strict mode): "VAR"`))
				}
			}
		})
	}
}

func TestEnvsubstPerDocument(t *testing.T) {
	g := NewWithT(t)
	deployment, err := os.ReadFile("testdata/envsubst/deployment.yaml")
	g.Expect(err).NotTo(HaveOccurred())
	disabled := strings.Replace(string(deployment), "metadata:\n", "metadata:\n  annotations:\n    kustomize.toolkit.fluxcd.io/substitute: disabled\n", 1)
	defaulted := strings.ReplaceAll(string(deployment), "${REPLICAS}", "${MISSING:=2}")
	malformed := strings.ReplaceAll(string(deployment), "${REPLICAS}", "${")
	v, err := New(Options{
		SchemaLocations: []string{"../../catalog/latest/" + DefaultSchemaLayout},
		Envsubst:        map[string]string{"REPLICAS": "2"},
	})
	g.Expect(err).NotTo(HaveOccurred())
	raw := strings.Join([]string{disabled, string(deployment), defaulted, malformed, string(deployment)}, "\n---\n")
	results := v.ValidateBytes(context.Background(), "input.yaml", []byte(raw))
	g.Expect(results).To(HaveLen(5))
	for i, reason := range []Reason{ReasonSchemaViolation, ReasonNone, ReasonNone, ReasonEnvsubstError, ReasonNone} {
		g.Expect(results[i].DocIndex).To(Equal(i + 1))
		g.Expect(results[i].Reason).To(Equal(reason))
	}
}

func TestEnvsubstErrorIdentity(t *testing.T) {
	for _, tt := range []struct {
		name   string
		raw    string
		want   string
		reason Reason
	}{
		{name: "no identity", raw: "value: '${'", want: "#1", reason: ReasonEnvsubstError},
		{name: "identity", raw: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\ndata:\n  value: '${'", want: "v1/ConfigMap/cm", reason: ReasonEnvsubstError},
		{name: "unparseable YAML is not substituted", raw: "value: [${", want: "#1", reason: ReasonYAMLParseError},
		{name: "duplicate keys are not substituted", raw: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\n  name: second\ndata:\n  value: '${'", want: "v1/ConfigMap/second", reason: ReasonYAMLParseError},
		{name: "comments are not substituted", raw: "# ${\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm # ${UNDEFINED}\n", want: "v1/ConfigMap/cm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			v, err := New(Options{
				SchemaLocations: []string{"../../catalog/latest/" + DefaultSchemaLayout},
				Envsubst:        map[string]string{},
				EnvsubstStrict:  true,
			})
			g.Expect(err).NotTo(HaveOccurred())
			results := v.ValidateBytes(context.Background(), "input.yaml", []byte(tt.raw))
			g.Expect(results).To(HaveLen(1))
			g.Expect(results[0].Reason).To(Equal(tt.reason))
			g.Expect(results[0].Identifier()).To(Equal(tt.want))
		})
	}
}

func TestNewEnvsubstNames(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{name: "VAR", valid: true},
		{name: "_", valid: true},
		{name: "_var_2", valid: true},
		{name: ""},
		{name: "2VAR"},
		{name: "VAR.NAME"},
		{name: "VAR-NAME"},
		{name: "VAR NAME"},
		{name: "VAR\n"},
		{name: "\u00e9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := New(Options{SchemaLocations: []string{"unused.json"}, Envsubst: map[string]string{tt.name: "value"}})
			if tt.valid {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring("var name is invalid")))
			}
		})
	}
}

func TestEnvsubstSkipKind(t *testing.T) {
	g := NewWithT(t)
	v, err := New(Options{
		SchemaLocations: []string{"unused.json"},
		SkipKinds:       []string{"ConfigMap"},
		Envsubst:        map[string]string{},
		EnvsubstStrict:  true,
	})
	g.Expect(err).NotTo(HaveOccurred())
	raw := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\ndata:\n  script: echo ${UNDEFINED}\n"
	results := v.ValidateBytes(context.Background(), "input.yaml", []byte(raw))
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusSkipped))
	g.Expect(results[0].Reason).To(Equal(ReasonKindSkipped))
}
