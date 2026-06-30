// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestTransformNode_SortsEnum(t *testing.T) {
	tests := []struct {
		name  string
		input []any
		want  []any
	}{
		{
			name:  "marker order is sorted",
			input: []any{"kubernetes-manifests", "kustomize-overlay", "helm-chart", "terraform-module"},
			want:  []any{"helm-chart", "kubernetes-manifests", "kustomize-overlay", "terraform-module"},
		},
		{
			name:  "already sorted is stable",
			input: []any{"invalid", "skipped", "valid"},
			want:  []any{"invalid", "skipped", "valid"},
		},
		{
			name:  "non-string enum is left untouched",
			input: []any{3, 1, 2},
			want:  []any{3, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			node := map[string]any{
				"type": "string",
				"enum": tt.input,
			}
			out, ok := transformNode(node).(map[string]any)
			g.Expect(ok).To(BeTrue())
			g.Expect(out["enum"]).To(Equal(tt.want))
		})
	}
}

func TestTransformNode_SortsNestedEnum(t *testing.T) {
	g := NewWithT(t)
	node := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": []any{"valid", "invalid", "skipped"},
			},
		},
	}
	out := transformNode(node).(map[string]any)
	status := out["properties"].(map[string]any)["status"].(map[string]any)
	g.Expect(status["enum"]).To(Equal([]any{"invalid", "skipped", "valid"}))
}
