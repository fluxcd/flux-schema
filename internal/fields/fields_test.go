// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package fields

import (
	"encoding/json"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFlattenMap_HeaderVariants(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "namespaced",
			opts: Options{
				GVK: schema.GroupVersionKind{
					Group:   "source.toolkit.fluxcd.io",
					Version: "v1",
					Kind:    "GitRepository",
				},
				Scope: ScopeNamespaced,
			},
			want: "apiVersion <string> enum=source.toolkit.fluxcd.io/v1\n" +
				"kind <string> enum=GitRepository\n" +
				"metadata.name <string> (required)\n" +
				"metadata.namespace <string> (required)\n",
		},
		{
			name: "cluster",
			opts: Options{
				GVK: schema.GroupVersionKind{
					Group:   "apiextensions.k8s.io",
					Version: "v1",
					Kind:    "CustomResourceDefinition",
				},
				Scope: ScopeCluster,
			},
			want: "apiVersion <string> enum=apiextensions.k8s.io/v1\n" +
				"kind <string> enum=CustomResourceDefinition\n" +
				"metadata.name <string> (required)\n",
		},
		{
			name: "unknown scope",
			opts: Options{
				GVK: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1",
					Kind:    "Widget",
				},
			},
			want: "apiVersion <string> enum=example.com/v1\n" +
				"kind <string> enum=Widget\n" +
				"metadata.name <string> (required)\n" +
				"metadata.namespace <string>\n",
		},
		{
			name: "core group",
			opts: Options{
				GVK: schema.GroupVersionKind{
					Version: "v1",
					Kind:    "Pod",
				},
				Scope: ScopeNamespaced,
			},
			want: "apiVersion <string> enum=v1\n" +
				"kind <string> enum=Pod\n" +
				"metadata.name <string> (required)\n" +
				"metadata.namespace <string> (required)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got, err := FlattenMap(map[string]any{}, tt.opts)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestFlattenMap_WalkAndAnnotations(t *testing.T) {
	g := NewWithT(t)
	root := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"zeta": map[string]any{
				"type":        "string",
				"description": "last",
			},
			"alpha": map[string]any{
				"type":        "string",
				"description": "first",
			},
			"spec": map[string]any{
				"type":     "object",
				"required": []any{"name", "modes"},
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Name\n\twith   spaces",
					},
					"modes": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
						"enum": []any{"fast", "slow", json.Number("3"), true, nil, []any{"x"}},
					},
					"count": map[string]any{
						"type":    "integer",
						"default": json.Number("3"),
					},
					"selector": map[string]any{
						"type":    "string",
						"default": "<tag>",
					},
					"status": map[string]any{
						"type":    "object",
						"default": map[string]any{"observedGeneration": json.Number("-1")},
					},
					"nothing": map[string]any{
						"type":    "string",
						"default": nil,
					},
				},
			},
		},
	}

	got, err := FlattenMap(root, namespacedGVK())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("apiVersion <string> enum=example.com/v1\n" +
		"kind <string> enum=Widget\n" +
		"metadata.name <string> (required)\n" +
		"metadata.namespace <string> (required)\n" +
		"alpha <string>\t# first\n" +
		"spec <object>\n" +
		"spec.count <integer> default=3\n" +
		"spec.modes <[]string> (required) enum=fast|slow|3|true|null|[\"x\"]\n" +
		"spec.name <string> (required)\t# Name with spaces\n" +
		"spec.nothing <string> default=null\n" +
		"spec.selector <string> default=\"<tag>\"\n" +
		"spec.status <object (free-form)> default={\"observedGeneration\":-1}\n" +
		"zeta <string>\t# last\n"))
}

func TestFlattenMap_RecursionPrefixesAndRootSkips(t *testing.T) {
	g := NewWithT(t)
	root := map[string]any{
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"containers": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []any{"name"},
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
							},
						},
					},
					"template": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"metadata": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"labels": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "string",
										},
									},
								},
							},
						},
					},
					"tenants": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type":     "object",
							"required": []any{"role"},
							"properties": map[string]any{
								"role": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}

	got, err := FlattenMap(root, namespacedGVK())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(ContainSubstring("spec.containers[].name <string> (required)\n"))
	g.Expect(got).To(ContainSubstring("spec.template.metadata <object>\n"))
	g.Expect(got).To(ContainSubstring("spec.template.metadata.labels <map[string]string>\n"))
	g.Expect(got).To(ContainSubstring("spec.tenants <map[string]object>\n"))
	g.Expect(got).To(ContainSubstring("spec.tenants.<key>.role <string> (required)\n"))
	g.Expect(strings.Count(got, "apiVersion <string>")).To(Equal(1))
	g.Expect(strings.Count(got, "kind <string>")).To(Equal(1))
	g.Expect(got).ToNot(ContainSubstring("\nmetadata <"))
}

func TestFlattenMap_DoesNotMutateInput(t *testing.T) {
	g := NewWithT(t)
	root := map[string]any{
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec": map[string]any{
				"type": "object",
			},
		},
	}
	before, err := json.Marshal(root)
	g.Expect(err).ToNot(HaveOccurred())

	_, err = FlattenMap(root, namespacedGVK())
	g.Expect(err).ToNot(HaveOccurred())

	after, err := json.Marshal(root)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(after).To(MatchJSON(before))
}

func TestFlatten_EquivalentToFlattenMap(t *testing.T) {
	g := NewWithT(t)
	schemaJSON := []byte(`{
	  "properties": {
	    "spec": {
	      "type": "object",
	      "properties": {
	        "replicas": {"type": "integer", "default": 1}
	      }
	    }
	  }
	}`)
	var root map[string]any
	decoderErr := json.Unmarshal(schemaJSON, &root)
	g.Expect(decoderErr).ToNot(HaveOccurred())
	root["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["replicas"].(map[string]any)["default"] = json.Number("1")

	fromJSON, err := Flatten(schemaJSON, namespacedGVK())
	g.Expect(err).ToNot(HaveOccurred())
	fromMap, err := FlattenMap(root, namespacedGVK())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(fromJSON).To(Equal(fromMap))
}

func TestFlatten_Errors(t *testing.T) {
	tests := []struct {
		name       string
		schemaJSON []byte
		opts       Options
		want       string
	}{
		{
			name:       "root not object",
			schemaJSON: []byte(`[]`),
			opts:       namespacedGVK(),
			want:       "schema root must be an object",
		},
		{
			name:       "empty kind",
			schemaJSON: []byte(`{}`),
			opts: Options{
				GVK: schema.GroupVersionKind{Group: "example.com", Version: "v1"},
			},
			want: "GVK kind is required",
		},
		{
			name:       "empty version",
			schemaJSON: []byte(`{}`),
			opts: Options{
				GVK: schema.GroupVersionKind{Group: "example.com", Kind: "Widget"},
			},
			want: "GVK version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := Flatten(tt.schemaJSON, tt.opts)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.want))
		})
	}
}

func namespacedGVK() Options {
	return Options{
		GVK: schema.GroupVersionKind{
			Group:   "example.com",
			Version: "v1",
			Kind:    "Widget",
		},
		Scope: ScopeNamespaced,
	}
}
