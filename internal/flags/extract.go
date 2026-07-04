// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package flags

import "github.com/spf13/cobra"

// DefaultExtractFormat is the per-group output-path template.
const DefaultExtractFormat = "{{ .Group }}/{{ .Kind }}_{{ .Version }}.json"

// ExtractOutput holds the output-shaping flags that
// are identical across `extract k8s` and `extract crd`.
type ExtractOutput struct {
	Dir              string
	Format           string
	StripDescription bool
	WithFieldIndex   bool
	IndexSource      string
}

// NewExtractOutput returns an ExtractOutput populated with the default values
// used when no flags are passed: current directory, per-group output template,
// descriptions preserved.
func NewExtractOutput() ExtractOutput {
	return ExtractOutput{
		Dir:    ".",
		Format: DefaultExtractFormat,
	}
}

// Register wires the shared extract output flags onto cmd with completion
// hints and help text.
func (e *ExtractOutput) Register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&e.Dir, "output-dir", "d", e.Dir,
		"directory where JSON Schema files are written (created if missing)")
	cmd.Flags().StringVarP(&e.Format, "output-format", "f", e.Format,
		"Go template for the output file path, relative to --output-dir; "+
			"variables: .Group, .GroupPrefix, .Kind, .Version")
	cmd.Flags().BoolVar(&e.StripDescription, "strip-description", e.StripDescription,
		"strip description fields from the extracted schemas to reduce their size")
	cmd.Flags().BoolVar(&e.WithFieldIndex, "with-field-index", e.WithFieldIndex,
		"also write a .fields.txt field index next to each schema: one greppable line "+
			"per field with its dotted path, type, constraints, and description; "+
			"map values are addressed as path.<key>.field")
	cmd.Flags().StringVar(&e.IndexSource, "index-source", e.IndexSource,
		"source name and version recorded in the field index header, overriding auto-detection "+
			"(e.g. 'my-operator v1.2.3')")
	_ = cmd.MarkFlagDirname("output-dir")
}
