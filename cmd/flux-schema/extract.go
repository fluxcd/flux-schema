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

// stripExplainMetadataForOutput applies the requested explain metadata level
// before the schema is written.
func stripExplainMetadataForOutput(node any, out flags.ExtractOutput) {
	switch {
	case out.WithExplainMetadata:
		return
	case out.WithExplainTypeMetadata:
		extractor.StripExplainLookupMetadata(node)
	default:
		extractor.StripExplainMetadata(node)
	}
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
	var explainMetadataSchemas []extractor.Schema
	for _, schemaDoc := range schemas {
		var index string
		if out.WithFieldIndex && !strings.HasSuffix(schemaDoc.Kind, "List") {
			var err error
			index, err = flattenSchema(schemaDoc, out.IndexSource)
			if err != nil {
				failures = append(failures, err)
			}
		}

		stripExplainMetadataForOutput(schemaDoc.JSON, out)
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
			explainMetadataSchemas = append(explainMetadataSchemas, schemaDoc)
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
		metadataPaths, err := writeExplainMetadata(explainMetadataSchemas, destDir)
		if err != nil {
			failures = append(failures, err)
		} else {
			for _, metadataPath := range metadataPaths {
				cmd.Printf("OK   %s\n", metadataPath)
			}
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

type explainReferenceDocument struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Targets    []explainReferenceTarget `json:"targets"`
}

type explainReferenceTarget struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

type explainCompletionShard struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Resources  []explainCompletionResource `json:"resources"`
}

type explainCompletionResource struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

func writeExplainMetadata(schemas []extractor.Schema, destDir string) ([]string, error) {
	var written []string
	references := explainReferences(schemas)
	for _, key := range sortedKeys(references) {
		relPath := filepath.Join(explainer.MetadataDir, explainer.ReferencesDir, key+".json")
		if err := writeJSONDocument(filepath.Join(destDir, relPath), explainReferenceDocument{
			APIVersion: "schema.plugin.fluxcd.io/v1beta1",
			Kind:       "ExplainResourceReference",
			Targets:    references[key],
		}); err != nil {
			return nil, err
		}
		written = append(written, relPath)
	}

	shards := explainCompletionShards(schemas)
	for _, key := range sortedKeys(shards) {
		relPath := filepath.Join(explainer.MetadataDir, explainer.CompletionDir, key+".json")
		if err := writeJSONDocument(filepath.Join(destDir, relPath), explainCompletionShard{
			APIVersion: "schema.plugin.fluxcd.io/v1beta1",
			Kind:       "ExplainCompletionShard",
			Resources:  shards[key],
		}); err != nil {
			return nil, err
		}
		written = append(written, relPath)
	}
	return written, nil
}

func explainReferences(schemas []extractor.Schema) map[string][]explainReferenceTarget {
	refs := map[string]map[string]explainReferenceTarget{}
	for _, schemaDoc := range schemas {
		if !hasDiscoveryResource(schemaDoc.Resource) {
			continue
		}
		target := explainReferenceTarget{Group: schemaDoc.Group, Version: schemaDoc.Version, Kind: schemaDoc.Kind}
		for _, alias := range explainAliases(schemaDoc) {
			if refs[alias] == nil {
				refs[alias] = map[string]explainReferenceTarget{}
			}
			refs[alias][targetKey(target)] = target
		}
	}
	out := make(map[string][]explainReferenceTarget, len(refs))
	for alias, targets := range refs {
		for _, key := range sortedKeys(targets) {
			out[alias] = append(out[alias], targets[key])
		}
	}
	return out
}

func explainCompletionShards(schemas []extractor.Schema) map[string][]explainCompletionResource {
	shards := map[string]map[string]explainCompletionResource{}
	for _, schemaDoc := range schemas {
		if !hasDiscoveryResource(schemaDoc.Resource) {
			continue
		}
		name := canonicalExplainName(schemaDoc)
		if name == "" {
			continue
		}
		resource := explainCompletionResource{Name: name, Aliases: explainAliases(schemaDoc)}
		for _, alias := range resource.Aliases {
			for _, key := range explainShardKeys(alias) {
				if shards[key] == nil {
					shards[key] = map[string]explainCompletionResource{}
				}
				shards[key][name] = mergeCompletionResource(shards[key][name], resource)
			}
		}
	}
	out := make(map[string][]explainCompletionResource, len(shards))
	for key, resources := range shards {
		for _, name := range sortedKeys(resources) {
			resource := resources[name]
			sort.Strings(resource.Aliases)
			out[key] = append(out[key], resource)
		}
	}
	return out
}

func hasDiscoveryResource(resource extractor.ResourceNames) bool {
	return resource.Singular != "" || resource.Plural != "" || len(resource.ShortNames) > 0
}

func explainAliases(schemaDoc extractor.Schema) []string {
	seen := map[string]bool{}
	var out []string
	add := func(alias string) {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" || seen[alias] {
			return
		}
		seen[alias] = true
		out = append(out, alias)
	}
	bases := make([]string, 0, 3+len(schemaDoc.Resource.ShortNames))
	bases = append(bases, schemaDoc.Kind, schemaDoc.Resource.Singular, schemaDoc.Resource.Plural)
	bases = append(bases, schemaDoc.Resource.ShortNames...)
	for _, alias := range bases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" {
			continue
		}
		add(alias)
		if schemaDoc.Group != "" {
			add(alias + "." + strings.ToLower(schemaDoc.Group))
		}
	}
	return out
}

func canonicalExplainName(schemaDoc extractor.Schema) string {
	name := strings.ToLower(strings.TrimSpace(schemaDoc.Resource.Plural))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(schemaDoc.Resource.Singular))
	}
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(schemaDoc.Kind))
	}
	if name == "" || strings.HasSuffix(name, "list") {
		return ""
	}
	if schemaDoc.Group == "" {
		return name
	}
	return name + "." + strings.ToLower(schemaDoc.Group)
}

func explainShardKeys(alias string) []string {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" || !validShardPrefix(alias[:1]) {
		return nil
	}
	keys := []string{alias[:1]}
	if len(alias) >= 2 && validShardPrefix(alias[:2]) {
		keys = append(keys, alias[:2])
	}
	return keys
}

func validShardPrefix(prefix string) bool {
	for _, r := range prefix {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func mergeCompletionResource(a, b explainCompletionResource) explainCompletionResource {
	if a.Name == "" {
		return b
	}
	seen := map[string]bool{}
	for _, alias := range a.Aliases {
		seen[alias] = true
	}
	for _, alias := range b.Aliases {
		if !seen[alias] {
			a.Aliases = append(a.Aliases, alias)
		}
	}
	return a
}

func targetKey(target explainReferenceTarget) string {
	return target.Group + "/" + target.Version + "/" + target.Kind
}

func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeJSONDocument(path string, doc any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
