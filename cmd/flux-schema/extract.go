// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"

	explainer "github.com/fluxcd/flux-schema/internal/explain"
	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/fields"
	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/tmpl"
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract JSON Schemas from Kubernetes API sources",
}

func init() {
	rootCmd.AddCommand(extractCmd)
}

// runSwaggerExtract is the shared `extract k8s`/`extract openshift` pipeline:
// it creates the output dir, runs extract on the swagger data, optionally
// strips descriptions, writes each schema, and reports per-document failures.
func runSwaggerExtract(cmd *cobra.Command, source string, data []byte, out flags.ExtractOutput,
	extract func([]byte) ([]extractor.Schema, []error),
) error {
	destDir := out.Dir
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	cmd.Printf("reading %s\n", source)

	schemas, errs := extract(data)

	var failures []error
	for _, e := range errs {
		failures = append(failures, fmt.Errorf("%s: %w", source, e))
	}

	written := 0
	var explainIndexSchemas []extractor.Schema
	for _, schemaDoc := range schemas {
		var index string
		if out.WithFieldIndex && !strings.HasSuffix(schemaDoc.Kind, "List") {
			var err error
			index, err = flattenSchema(schemaDoc, out.IndexSource)
			if err != nil {
				failures = append(failures, err)
			}
		}

		if !out.WithExplainMetadata {
			extractor.StripExplainMetadata(schemaDoc.JSON)
		}
		if out.StripDescription {
			extractor.StripDescriptions(schemaDoc.JSON)
		}

		relPath, err := writeSwaggerSchema(schemaDoc, destDir, out.Format)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		cmd.Printf("OK   %s\n", relPath)
		written++
		if out.WithExplainMetadata {
			explainIndexSchemas = append(explainIndexSchemas, schemaDoc)
		}

		if out.WithExplainMetadata {
			aliasPaths, err := writeExplainAliases(schemaDoc, destDir, out.Format, relPath)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			for _, aliasPath := range aliasPaths {
				cmd.Printf("OK   %s\n", aliasPath)
			}
		}

		if out.WithFieldIndex && index != "" {
			indexRelPath, err := writeFieldIndex(destDir, relPath, index)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			cmd.Printf("OK   %s\n", indexRelPath)
		}
	}

	if out.WithExplainMetadata {
		indexRelPath, err := writeExplainIndex(explainIndexSchemas, destDir)
		if err != nil {
			failures = append(failures, err)
		} else if indexRelPath != "" {
			cmd.Printf("OK   %s\n", indexRelPath)
		}
	}

	cmd.Printf("Summary: %d schemas extracted\n", written)

	if len(failures) > 0 {
		for _, e := range failures {
			cmd.PrintErrf("FAIL %v\n", e)
		}
		return fmt.Errorf("%d error(s) during extraction", len(failures))
	}
	return nil
}

func flattenSchema(schemaDoc extractor.Schema, indexSource string) (string, error) {
	source := schemaDoc.Source
	if indexSource != "" {
		source = indexSource
	}
	return fields.FlattenMap(schemaDoc.JSON, fields.Options{
		GVK: k8sschema.GroupVersionKind{
			Group:   schemaDoc.Group,
			Version: schemaDoc.Version,
			Kind:    schemaDoc.Kind,
		},
		Scope:              schemaDoc.Scope,
		Source:             source,
		Deprecated:         schemaDoc.Deprecated,
		DeprecationWarning: schemaDoc.DeprecationWarning,
	})
}

func fieldIndexRelPath(relPath string) string {
	if strings.HasSuffix(relPath, ".json") {
		return strings.TrimSuffix(relPath, ".json") + ".fields.txt"
	}
	return relPath + ".fields.txt"
}

func writeFieldIndex(destDir, relPath, index string) (string, error) {
	indexRelPath := fieldIndexRelPath(relPath)
	outPath := filepath.Join(destDir, indexRelPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}
	if err := os.WriteFile(outPath, []byte(index), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return indexRelPath, nil
}

type explainIndexDocument struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Resources  []explainIndexResource `json:"resources"`
}

type explainIndexResource struct {
	Group      string   `json:"group,omitempty"`
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Singular   string   `json:"singular,omitempty"`
	Plural     string   `json:"plural,omitempty"`
	ShortNames []string `json:"shortNames,omitempty"`
	Scope      string   `json:"scope,omitempty"`
}

func writeExplainIndex(schemas []extractor.Schema, destDir string) (string, error) {
	resources := explainIndexResources(schemas)
	if len(resources) == 0 {
		return "", nil
	}
	doc := explainIndexDocument{
		APIVersion: "schema.plugin.fluxcd.io/v1beta1",
		Kind:       "ExplainIndex",
		Resources:  resources,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	outPath := filepath.Join(destDir, explainer.IndexFileName)
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return explainer.IndexFileName, nil
}

func explainIndexResources(schemas []extractor.Schema) []explainIndexResource {
	seen := map[string]bool{}
	resources := make([]explainIndexResource, 0, len(schemas))
	for _, schemaDoc := range schemas {
		if !hasDiscoveryResource(schemaDoc.Resource) {
			continue
		}
		key := schemaDoc.Group + "/" + schemaDoc.Version + "/" + schemaDoc.Kind
		if seen[key] {
			continue
		}
		seen[key] = true
		resources = append(resources, explainIndexResource{
			Group:      schemaDoc.Group,
			Version:    schemaDoc.Version,
			Kind:       schemaDoc.Kind,
			Singular:   schemaDoc.Resource.Singular,
			Plural:     schemaDoc.Resource.Plural,
			ShortNames: append([]string(nil), schemaDoc.Resource.ShortNames...),
			Scope:      schemaDoc.Scope,
		})
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Group != resources[j].Group {
			return resources[i].Group < resources[j].Group
		}
		if resources[i].Version != resources[j].Version {
			return resources[i].Version < resources[j].Version
		}
		return resources[i].Kind < resources[j].Kind
	})
	return resources
}

func hasDiscoveryResource(resource extractor.ResourceNames) bool {
	return resource.Singular != "" || resource.Plural != "" || len(resource.ShortNames) > 0
}

func writeExplainAliases(schema extractor.Schema, destDir, format, canonicalRelPath string) ([]string, error) {
	aliases := schema.ExplainAliases()
	if len(aliases) == 0 {
		return nil, nil
	}
	var written []string
	for _, alias := range aliases {
		rendered, err := tmpl.Render(format, tmpl.SchemaVars{
			Group:   schema.Group,
			Kind:    alias,
			Version: schema.Version,
		})
		if err != nil {
			return nil, fmt.Errorf("%s/%s %s alias %s: %w", schema.Group, schema.Kind, schema.Version, alias, err)
		}
		relPath := filepath.FromSlash(rendered)
		if relPath == canonicalRelPath {
			continue
		}
		outPath := filepath.Join(destDir, relPath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
		}
		payload, err := marshalSchema(map[string]any{
			extractor.KeyFluxSchemaAlias: map[string]any{
				"group":   schema.Group,
				"version": schema.Version,
				"kind":    schema.Kind,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("%s alias %s: %w", schema.Kind, alias, err)
		}
		if err := os.WriteFile(outPath, payload, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", outPath, err)
		}
		written = append(written, relPath)
	}
	return written, nil
}

// writeSwaggerSchema renders the output template, writes the schema as
// pretty-printed JSON under destDir, and returns the path relative to
// destDir. Shared by `extract k8s`, `extract openshift`, and `extract crd`.
func writeSwaggerSchema(schema extractor.Schema, destDir, format string) (string, error) {
	rendered, err := tmpl.Render(format, tmpl.SchemaVars{
		Group:   schema.Group,
		Kind:    schema.Kind,
		Version: schema.Version,
	})
	if err != nil {
		return "", fmt.Errorf("%s/%s %s: %w", schema.Group, schema.Kind, schema.Version, err)
	}

	relPath := filepath.FromSlash(rendered)
	outPath := filepath.Join(destDir, relPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}

	payload, err := marshalSchema(schema.JSON)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", schema.Kind, schema.Version, err)
	}
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return relPath, nil
}

// marshalSchema encodes a parsed schema as deterministic, pretty-printed JSON.
// encoding/json sorts map[string]any keys alphabetically, so output is stable across runs.
func marshalSchema(schema map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
