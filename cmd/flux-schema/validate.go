// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/validator"
)

var validateCmd = &cobra.Command{
	Use:   "validate [paths...]",
	Short: "Validate Kubernetes manifests against JSON Schemas",
	Example: `  # Validate YAMLs under ./manifests against the default catalog
  # The default catalog covers the latest stable Kubernetes and Flux APIs
  # https://github.com/fluxcd/flux-schema/tree/main/catalog
  flux-schema validate ./manifests --verbose

  # Validate against local schemas only
  flux-schema validate ./manifests \
    --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json' \
    --skip-missing-schemas

  # Combine the default catalog with local CRD schemas (use 'default' as an alias)
  flux-schema validate ./manifests \
    --schema-location default \
    --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'

  # Read manifests from a pipe
  kustomize build . | flux-schema validate /dev/stdin \
    --schema-location default \
    --schema-location './schemas/{{.Group}}/{{.Kind}}_{{.Version}}.json'`,
	Args: cobra.MinimumNArgs(1),
	RunE: validateCmdRun,
}

type validateFlags struct {
	schemaLocations    []string
	skipMissingSchemas bool
	verbose            bool
}

// defaultValidateSchemaLocation points at the flux-schema catalog,
// covering the latest stable Kubernetes and Flux APIs.
// It is used when no --schema-location is provided, and is what
// the literal value "default" expands to in --schema-location.
const defaultValidateSchemaLocation = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/catalog/latest/{{.Group}}/{{.Kind}}_{{.Version}}.json"

var validateArgs = validateFlags{}

const stdinPath = "/dev/stdin"

func init() {
	validateCmd.Flags().StringArrayVar(&validateArgs.schemaLocations, "schema-location", nil,
		"template URL or file path for schemas (repeatable); use 'default' for the built-in catalog")
	validateCmd.Flags().BoolVar(&validateArgs.skipMissingSchemas, "skip-missing-schemas", false,
		"skip documents for which no schema can be found instead of failing")
	validateCmd.Flags().BoolVarP(&validateArgs.verbose, "verbose", "v", false,
		"print a line for every document, including valid and skipped")
	rootCmd.AddCommand(validateCmd)
}

func validateCmdRun(cmd *cobra.Command, args []string) error {
	locations := validateArgs.schemaLocations
	if len(locations) == 0 {
		locations = []string{defaultValidateSchemaLocation}
	} else {
		expanded, err := expandSchemaLocations(locations)
		if err != nil {
			return err
		}
		locations = expanded
	}

	v, err := validator.New(validator.Options{
		SchemaLocations:    locations,
		SkipMissingSchemas: validateArgs.skipMissingSchemas,
		HTTPTimeout:        rootArgs.timeout,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	stdinOnly := len(args) == 1 && args[0] == stdinPath

	files := make(map[string]struct{})
	var nValid, nInvalid, nSkipped int

	// Workers process documents concurrently, so results arrive out of order.
	// Collect them all, then emit sorted by (Source, DocIndex) so CLI output
	// is deterministic across runs.
	var collected []validator.Result
	for r := range v.ValidateSources(ctx, args) {
		files[r.Source] = struct{}{}
		switch r.Status {
		case validator.StatusValid:
			nValid++
		case validator.StatusInvalid:
			nInvalid++
		case validator.StatusSkipped:
			nSkipped++
		}
		collected = append(collected, r)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].Source != collected[j].Source {
			return collected[i].Source < collected[j].Source
		}
		return collected[i].DocIndex < collected[j].DocIndex
	})
	for _, r := range collected {
		if shouldPrint(r.Status, validateArgs.verbose) {
			writeResult(cmd, r)
		}
	}

	writeSummary(cmd, len(files), nValid+nInvalid+nSkipped, nValid, nInvalid, nSkipped, stdinOnly)

	if nInvalid > 0 {
		// Summary line already communicates the failure; exit non-zero
		// via errSilent so we don't print a redundant "✗ ..." line.
		return errSilent
	}
	return nil
}

// expandSchemaLocations replaces every case-insensitive "default" with
// defaultValidateSchemaLocation, preserving order so the user controls
// fallback priority (e.g. --schema-location default --schema-location ./local
// keeps the catalog first).
func expandSchemaLocations(locations []string) ([]string, error) {
	out := make([]string, len(locations))
	for i, loc := range locations {
		if loc == "" {
			return nil, fmt.Errorf("--schema-location must not be empty")
		}
		if strings.EqualFold(loc, "default") {
			out[i] = defaultValidateSchemaLocation
		} else {
			out[i] = loc
		}
	}
	return out, nil
}

// shouldPrint returns true when this result should be written to stdout.
// Quiet mode (default) only emits invalid lines; --verbose emits every status.
func shouldPrint(s validator.Status, verbose bool) bool {
	if verbose {
		return true
	}
	return s == validator.StatusInvalid
}

func writeResult(cmd *cobra.Command, r validator.Result) {
	verb := "is invalid"
	switch r.Status {
	case validator.StatusValid:
		verb = "is valid"
	case validator.StatusSkipped:
		verb = "is skipped"
	}
	if r.Message != "" {
		cmd.Printf("%s - %s %s: %s\n", r.Source, r.Identifier(), verb, r.Message)
	} else {
		cmd.Printf("%s - %s %s\n", r.Source, r.Identifier(), verb)
	}
	for _, e := range r.Errors {
		if e.Path == "" {
			cmd.Printf("  - %s\n", e.Msg)
		} else {
			cmd.Printf("  - %s: %s\n", e.Path, e.Msg)
		}
	}
}

func writeSummary(cmd *cobra.Command, nFiles, nResources, nValid, nInvalid, nSkipped int, stdinOnly bool) {
	resources := pluralize("resource", nResources)
	if stdinOnly {
		cmd.Printf("Summary: %d %s found parsing stdin - Valid: %d, Invalid: %d, Skipped: %d\n",
			nResources, resources, nValid, nInvalid, nSkipped)
		return
	}
	files := pluralize("file", nFiles)
	cmd.Printf("Summary: %d %s found in %d %s - Valid: %d, Invalid: %d, Skipped: %d\n",
		nResources, resources, nFiles, files, nValid, nInvalid, nSkipped)
}

func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
