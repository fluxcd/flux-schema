// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package fields

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestTypeString_DecisionTable(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{
			name:   "x-kubernetes-int-or-string",
			schema: map[string]any{"x-kubernetes-int-or-string": true},
			want:   "<int-or-string>",
		},
		{
			name: "array with scalar items",
			schema: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			want: "<[]string>",
		},
		{
			name: "array with object items",
			schema: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "object"},
			},
			want: "<[]object>",
		},
		{
			name: "map with object values",
			schema: map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				},
			},
			want: "<map[string]object>",
		},
		{
			name: "map with scalar values",
			schema: map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "integer",
				},
			},
			want: "<map[string]integer>",
		},
		{
			name: "map with typed object values without properties",
			schema: map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "object",
				},
			},
			want: "<map[string]object>",
		},
		{
			name: "free-form object",
			schema: map[string]any{
				"type": "object",
			},
			want: "<object (free-form)>",
		},
		{
			name: "object with properties",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
			want: "<object>",
		},
		{
			name: "single scalar type",
			schema: map[string]any{
				"type": "boolean",
			},
			want: "<boolean>",
		},
		{
			name:   "no type",
			schema: map[string]any{},
			want:   "<any>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(typeString(tt.schema)).To(Equal(tt.want))
		})
	}
}

func TestTypeString_NormalizesNullableShapes(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{
			name:   "nullable single type",
			schema: map[string]any{"type": []any{"string", "null"}},
			want:   "<string>",
		},
		{
			name:   "oneOf int-or-string",
			schema: map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}},
			want:   "<int-or-string>",
		},
		{
			name: "nullable oneOf int-or-string",
			schema: map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
					map[string]any{"type": "null"},
				},
			},
			want: "<int-or-string>",
		},
		{
			name:   "type array int-or-string",
			schema: map[string]any{"type": []any{"null", "integer", "string"}},
			want:   "<int-or-string>",
		},
		{
			name: "nullable array items",
			schema: map[string]any{
				"type":  []any{"array", "null"},
				"items": map[string]any{"type": []any{"number", "null"}},
			},
			want: "<[]number>",
		},
		{
			name: "additionalProperties false without properties is free-form",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			want: "<object (free-form)>",
		},
		{
			name: "additionalProperties false with properties is object",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			want: "<object>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(typeString(tt.schema)).To(Equal(tt.want))
		})
	}
}

func TestStringification(t *testing.T) {
	g := NewWithT(t)

	value, err := stringifyEnumValue("plain")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("plain"))
	value, err = stringifyEnumValue("two words")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal(`"two words"`))
	value, err = stringifyEnumValue("a|b")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal(`"a|b"`))
	value, err = stringifyEnumValue(json.Number("10.5"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("10.5"))
	value, err = stringifyEnumValue(true)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("true"))
	value, err = stringifyEnumValue(false)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("false"))
	value, err = stringifyEnumValue(nil)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("null"))
	value, err = stringifyEnumValue(map[string]any{"x": []any{"y"}})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal(`{"x":["y"]}`))

	value, err = stringifyDefault(nil)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal("null"))
	value, err = stringifyDefault("<value>")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal(`"<value>"`))
	value, err = stringifyDefault(map[string]any{"x": []any{json.Number("1")}})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(value).To(Equal(`{"x":[1]}`))
}

func TestCleanDescription(t *testing.T) {
	g := NewWithT(t)
	g.Expect(cleanDescription("one\n\t two   three\tfour")).To(Equal("one two three four"))
}
