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

	"github.com/fluxcd/pkg/tar"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/tmpl"
)

var extractCRDCmd = &cobra.Command{
	Use:   "crd [files...]",
	Short: "Extract JSON Schemas from Kubernetes CRD YAML files",
	Example: `  # Extract schemas using the datreeio CRDs-catalog layout (default)
  kubectl get crd ocirepositories.source.toolkit.fluxcd.io -o yaml > oci-crd.yaml
  flux-schema extract crd oci-crd.yaml -d ./schemas

  # Extract using the kubeval / kubeconform flat layout
  kubectl get crds -o yaml > crds.yaml
  flux-schema extract crd crds.yaml \
    --output-format '{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json'

  # Extract all schemas and store them in a tar.gz archive
  kustomize build config/crd | flux-schema extract crd /dev/stdin \
    --output-archive dist/crd-schemas.tar.gz`,
	Args: cobra.MinimumNArgs(1),
	RunE: extractCRDCmdRun,
}

type extractCRDFlags struct {
	flags.ExtractOutput
	outputArchive string
}

var extractCRDArgs = extractCRDFlags{
	ExtractOutput: flags.NewExtractOutput(),
}

func init() {
	extractCRDArgs.Register(extractCRDCmd)
	extractCRDCmd.Flags().StringVarP(&extractCRDArgs.outputArchive, "output-archive", "a", "",
		"path to a tar.gz file to write all schemas into; mutually exclusive with --output-dir")
	_ = extractCRDCmd.MarkFlagFilename("output-archive", "tar.gz", "tgz")
	extractCRDCmd.MarkFlagsMutuallyExclusive("output-dir", "output-archive")
	extractCmd.AddCommand(extractCRDCmd)
}

func extractCRDCmdRun(cmd *cobra.Command, args []string) error {
	archive := extractCRDArgs.outputArchive
	destDir := extractCRDArgs.Dir
	if archive != "" {
		if !hasArchiveExt(archive) {
			return fmt.Errorf("output archive %q must end in .tar.gz or .tgz", archive)
		}
		if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
			return fmt.Errorf("create archive parent dir: %w", err)
		}
		staging, err := os.MkdirTemp("", "flux-schema-extract-*")
		if err != nil {
			return fmt.Errorf("create staging dir: %w", err)
		}
		defer os.RemoveAll(staging)
		destDir = staging
	} else {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}

	var (
		failures []error
		written  int
	)
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", path, err))
			continue
		}
		crds, errs := extractor.ExtractCRDs(data)
		if extractCRDArgs.StripDescription {
			for _, c := range crds {
				extractor.StripDescriptions(c.JSON)
			}
		}
		for _, e := range errs {
			failures = append(failures, fmt.Errorf("%s: %w", path, e))
		}
		for _, crd := range crds {
			relPath, err := writeCRDSchema(path, crd, destDir)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			displayPath := filepath.Join(destDir, relPath)
			if archive != "" {
				displayPath = relPath
			}
			cmd.Printf("OK   %s -> %s\n", path, displayPath)
			written++
		}
	}

	if archive != "" {
		if err := writeArchive(archive, destDir); err != nil {
			failures = append(failures, err)
		} else {
			cmd.Printf("wrote %s (%d schema(s))\n", archive, written)
		}
	}

	if len(failures) > 0 {
		for _, e := range failures {
			cmd.PrintErrf("FAIL %v\n", e)
		}
		return fmt.Errorf("%d error(s) during extraction", len(failures))
	}
	return nil
}

func writeCRDSchema(srcPath string, crd extractor.Schema, destDir string) (string, error) {
	rendered, err := tmpl.Render(extractCRDArgs.Format, tmpl.SchemaVars{
		Group:   crd.Group,
		Kind:    crd.Kind,
		Version: crd.Version,
	})
	if err != nil {
		return "", fmt.Errorf("%s (%s/%s %s): %w", srcPath, crd.Group, crd.Kind, crd.Version, err)
	}

	relPath := filepath.FromSlash(rendered)
	outPath := filepath.Join(destDir, relPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(outPath), err)
	}

	payload, err := marshalSchema(crd.JSON)
	if err != nil {
		return "", fmt.Errorf("%s %s %s: %w", srcPath, crd.Kind, crd.Version, err)
	}
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}
	return relPath, nil
}

func hasArchiveExt(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

func writeArchive(archivePath, srcDir string) error {
	f, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive %s: %w", archivePath, err)
	}
	defer f.Close()
	if _, err := tar.Tar(srcDir, f); err != nil {
		return fmt.Errorf("write archive %s: %w", archivePath, err)
	}
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
