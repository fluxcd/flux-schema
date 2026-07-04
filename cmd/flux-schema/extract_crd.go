// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fluxcd/pkg/tar"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/flags"
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
  kustomize build config/crd | flux-schema extract crd \
    --output-archive dist/crd-schemas.tar.gz`,
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
	inputs, err := resolveStdinArgs(args)
	if err != nil {
		return err
	}

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
	for _, path := range inputs {
		data, err := readSource(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", path, err))
			continue
		}
		crds, errs := extractor.ExtractCRDs(data)
		for _, e := range errs {
			failures = append(failures, fmt.Errorf("%s: %w", path, e))
		}
		for _, crd := range crds {
			var index string
			if extractCRDArgs.WithFieldIndex {
				var err error
				index, err = flattenSchema(crd, extractCRDArgs.IndexSource)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", path, err))
				}
			}

			if extractCRDArgs.StripDescription {
				extractor.StripDescriptions(crd.JSON)
			}

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

			if extractCRDArgs.WithFieldIndex && index != "" {
				indexRelPath, err := writeFieldIndex(destDir, relPath, index)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", path, err))
					continue
				}
				displayPath := filepath.Join(destDir, indexRelPath)
				if archive != "" {
					displayPath = indexRelPath
				}
				cmd.Printf("OK   %s -> %s\n", path, displayPath)
			}
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
	relPath, err := writeSwaggerSchema(crd, destDir, extractCRDArgs.Format)
	if err != nil {
		return "", fmt.Errorf("%s: %w", srcPath, err)
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
