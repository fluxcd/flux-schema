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
	g.Expect(metadataProps["generateName"].(map[string]any)).To(HaveKeyWithValue("maxLength", 40))
	g.Expect(metadataProps).To(HaveKey("namespace"))
}
