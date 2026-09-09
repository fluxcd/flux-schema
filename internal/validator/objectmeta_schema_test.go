// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestAugmentRootObjectMetaSchema_FillsCompactMetadata(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"metadata": map[string]any{"type": "object"},
		},
	}

	augmentRootObjectMetaSchema(schema)

	metadata := schema["properties"].(map[string]any)["metadata"].(map[string]any)
	g.Expect(metadata["additionalProperties"]).To(BeFalse())

	metadataProps := metadata["properties"].(map[string]any)
	g.Expect(metadataProps).To(HaveKey("name"))
	g.Expect(metadataProps).To(HaveKey("generateName"))
	g.Expect(metadataProps).To(HaveKey("namespace"))
	g.Expect(metadataProps).To(HaveKey("labels"))
	g.Expect(metadataProps).To(HaveKey("annotations"))
	g.Expect(metadataProps).To(HaveKey("ownerReferences"))
	g.Expect(metadataProps).To(HaveKey("managedFields"))
	g.Expect(metadataProps["namespace"].(map[string]any)).To(HaveKeyWithValue("type", ConsistOf("string", jsonNullType)))
	g.Expect(metadataProps["labels"].(map[string]any)).To(HaveKeyWithValue("type", ConsistOf("object", jsonNullType)))
	g.Expect(metadataProps["finalizers"].(map[string]any)).To(HaveKeyWithValue("type", ConsistOf("array", jsonNullType)))
}

func TestAugmentRootObjectMetaSchema_PreservesAuthoredConstraints(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":      "string",
						"maxLength": 63,
					},
					"generateName": map[string]any{
						"type":      "string",
						"maxLength": 40,
					},
				},
			},
		},
	}

	augmentRootObjectMetaSchema(schema)

	metadata := schema["properties"].(map[string]any)["metadata"].(map[string]any)
	metadataProps := metadata["properties"].(map[string]any)
	g.Expect(metadataProps["name"].(map[string]any)).To(HaveKeyWithValue("maxLength", 63))
	g.Expect(metadataProps["name"].(map[string]any)).To(HaveKeyWithValue("type", ConsistOf("string", jsonNullType)))
	g.Expect(metadataProps["generateName"].(map[string]any)).To(HaveKeyWithValue("maxLength", 40))
	g.Expect(metadataProps["generateName"].(map[string]any)).To(HaveKeyWithValue("type", ConsistOf("string", jsonNullType)))
	g.Expect(metadataProps).To(HaveKey("namespace"))
}

func TestAugmentRootObjectMetaSchema_InlinesLocalRef(t *testing.T) {
	g := NewWithT(t)
	schema := map[string]any{
		"type": "object",
		"definitions": map[string]any{
			"ObjectMeta": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
		"properties": map[string]any{
			"metadata": map[string]any{"$ref": "#/definitions/ObjectMeta"},
		},
	}

	augmentRootObjectMetaSchema(schema)

	metadata := schema["properties"].(map[string]any)["metadata"].(map[string]any)
	g.Expect(metadata).ToNot(HaveKey("$ref"))
	g.Expect(metadata["additionalProperties"]).To(BeFalse())
	metadataProps := metadata["properties"].(map[string]any)
	g.Expect(metadataProps).To(HaveKey("name"))
	g.Expect(metadataProps).To(HaveKey("namespace"))

	definition := schema["definitions"].(map[string]any)["ObjectMeta"].(map[string]any)
	definitionProps := definition["properties"].(map[string]any)
	g.Expect(definitionProps).ToNot(HaveKey("namespace"), "only root metadata should be inlined and augmented")
}
