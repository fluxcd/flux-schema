// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/flag"
)

var extractRestrictionCmd = &cobra.Command{
	Use:   "restriction [files...]",
	Short: "Extract JSON Schemas from restriction sample YAML manifests",
	Example: `  # Extract restriction schema from a sample manifest
  flux-schema extract restriction my-restriction.yaml -d ./schemas

  # Extract using a custom layout
  flux-schema extract restriction my-restriction.yaml \
    --output-format '{{ .Group }}/{{ .Kind }}_{{ .Version }}_{{ .Restriction }}.json'

  # Extract schemas from stdin and package into a tar.gz archive
  cat my-restriction.yaml | flux-schema extract restriction \
    --output-archive dist/restriction-schemas.tar.gz`,
	RunE: extractRestrictionCmdRun,
}

type extractRestrictionFlags struct {
	flag.ExtractOutput
	outputArchive string
}

const defaultRestrictionFormat = "{{.Group}}/{{.Kind}}_{{.Version}}_{{.Restriction}}.json"

var extractRestrictionArgs = newExtractRestrictionFlags()

func newExtractRestrictionFlags() extractRestrictionFlags {
	f := extractRestrictionFlags{
		ExtractOutput: flag.NewExtractOutput(),
	}
	f.Format = defaultRestrictionFormat
	return f
}

func init() {
	extractRestrictionArgs.Register(extractRestrictionCmd)
	extractRestrictionCmd.Flags().StringVarP(&extractRestrictionArgs.outputArchive, "output-archive", "a", "",
		"path to a tar.gz file to write all schemas into; mutually exclusive with --output-dir")
	_ = extractRestrictionCmd.MarkFlagFilename("output-archive", "tar.gz", "tgz")
	extractRestrictionCmd.MarkFlagsMutuallyExclusive("output-dir", "output-archive")

	extractCmd.AddCommand(extractRestrictionCmd)
}

func extractRestrictionCmdRun(cmd *cobra.Command, args []string) error {
	inputs, err := resolveStdinArgs(args)
	if err != nil {
		return err
	}

	archive := extractRestrictionArgs.outputArchive
	destDir := extractRestrictionArgs.Dir
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
		failures               []error
		written                int
		explainMetadataSchemas []extractor.Schema
	)
	for _, path := range inputs {
		data, err := readSource(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", path, err))
			continue
		}
		schemas, errs := extractor.ExtractRestriction(data)
		for _, e := range errs {
			failures = append(failures, fmt.Errorf("%s: %w", path, e))
		}
		for _, schemaDoc := range schemas {
			var index string
			if extractRestrictionArgs.WithFieldIndex {
				var err error
				index, err = flattenSchema(schemaDoc, extractRestrictionArgs.IndexSource)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", path, err))
				}
			}

			stripExplainMetadataForOutput(schemaDoc.JSON, extractRestrictionArgs.ExtractOutput)
			if extractRestrictionArgs.StripDescription {
				extractor.StripDescriptions(schemaDoc.JSON)
			}

			relPath, err := writeSwaggerSchema(schemaDoc, destDir, extractRestrictionArgs.Format)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", path, err))
				continue
			}
			displayPath := filepath.Join(destDir, relPath)
			if archive != "" {
				displayPath = relPath
			}
			cmd.Printf("OK   %s -> %s\n", path, displayPath)
			written++
			if extractRestrictionArgs.WithExplainMetadata {
				explainMetadataSchemas = append(explainMetadataSchemas, schemaDoc)
			}

			if extractRestrictionArgs.WithExplainMetadata {
				aliasPaths, err := writeExplainAliases(schemaDoc, destDir, extractRestrictionArgs.Format, relPath)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", path, err))
					continue
				}
				for _, aliasPath := range aliasPaths {
					displayPath := filepath.Join(destDir, aliasPath)
					if archive != "" {
						displayPath = aliasPath
					}
					cmd.Printf("OK   %s -> %s\n", path, displayPath)
				}
			}

			if extractRestrictionArgs.WithFieldIndex && index != "" {
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

	if extractRestrictionArgs.WithExplainMetadata {
		metadataPaths, err := writeExplainMetadata(explainMetadataSchemas, destDir)
		if err != nil {
			failures = append(failures, err)
		} else {
			for _, metadataPath := range metadataPaths {
				displayPath := filepath.Join(destDir, metadataPath)
				if archive != "" {
					displayPath = metadataPath
				}
				cmd.Printf("OK   %s\n", displayPath)
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
