// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"

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
	for _, schemaDoc := range schemas {
		var index string
		if out.WithFieldIndex {
			var err error
			index, err = flattenSchema(schemaDoc)
			if err != nil {
				failures = append(failures, err)
			}
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

		if out.WithFieldIndex && index != "" {
			indexRelPath, err := writeFieldIndex(destDir, relPath, index)
			if err != nil {
				failures = append(failures, err)
				continue
			}
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

func flattenSchema(schemaDoc extractor.Schema) (string, error) {
	return fields.FlattenMap(schemaDoc.JSON, fields.Options{
		GVK: k8sschema.GroupVersionKind{
			Group:   schemaDoc.Group,
			Version: schemaDoc.Version,
			Kind:    schemaDoc.Kind,
		},
		Scope: schemaDoc.Scope,
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
