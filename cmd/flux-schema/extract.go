// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/tmpl"
)

const defaultExtractFormat = "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"

var extractCmd = &cobra.Command{
	Use:   "extract [crd-file...]",
	Short: "Extract JSON Schemas from Kubernetes CRD YAML files",
	Example: `  # Extract schemas using the kubeval / kubeconform layout
  kubectl get crd ocirepositories.source.toolkit.fluxcd.io -o yaml > oci-crd.yaml
  flux-schema extract oci-crd.yaml -d ./schemas \
    --output-format '{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json'

  # Extract in the current dir with one directory per API group
  kubectl get crds -o yaml > crds.yaml
  flux-schema extract crds.yaml \
    --output-format '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json'`,
	Args: cobra.MinimumNArgs(1),
	RunE: extractCmdRun,
}

type extractFlags struct {
	outputDir    string
	outputFormat string
}

var extractArgs = extractFlags{
	outputDir:    ".",
	outputFormat: defaultExtractFormat,
}

func init() {
	extractCmd.Flags().StringVarP(&extractArgs.outputDir, "output-dir", "d", extractArgs.outputDir,
		"directory where JSON Schema files are written (created if missing)")
	extractCmd.Flags().StringVarP(&extractArgs.outputFormat, "output-format", "f", defaultExtractFormat,
		"Go template for the output file path, relative to --output-dir; "+
			"variables: .Group, .GroupPrefix, .Kind, .Version")
	_ = extractCmd.MarkFlagDirname("output-dir")
	rootCmd.AddCommand(extractCmd)
}

func extractCmdRun(cmd *cobra.Command, args []string) error {
	if err := os.MkdirAll(extractArgs.outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	var failures []error
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", path, err))
			continue
		}
		crds, errs := extractor.Extract(data)
		for _, e := range errs {
			failures = append(failures, fmt.Errorf("%s: %w", path, e))
		}
		for _, crd := range crds {
			if err := writeCRD(cmd, path, crd); err != nil {
				failures = append(failures, err)
			}
		}
	}

	if len(failures) > 0 {
		for _, e := range failures {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "FAIL %v\n", e)
		}
		return fmt.Errorf("%d error(s) during extraction", len(failures))
	}
	return nil
}

func writeCRD(cmd *cobra.Command, srcPath string, crd extractor.CRD) error {
	rendered, err := tmpl.Render(extractArgs.outputFormat, tmpl.SchemaVars{
		Group:   crd.Group,
		Kind:    crd.Kind,
		Version: crd.Version,
	})
	if err != nil {
		return fmt.Errorf("%s (%s/%s %s): %w", srcPath, crd.Group, crd.Kind, crd.Version, err)
	}

	outPath := filepath.Join(extractArgs.outputDir, filepath.FromSlash(rendered))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}

	payload, err := marshalSchema(crd.Schema)
	if err != nil {
		return fmt.Errorf("%s %s %s: %w", srcPath, crd.Kind, crd.Version, err)
	}
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "OK   %s -> %s\n", srcPath, outPath)
	return nil
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
