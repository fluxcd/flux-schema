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

  # Validate against a local schema directory written by 'flux-schema extract'
  # (bare paths/URLs get '{{.Group}}/{{.Kind}}_{{.Version}}.json' appended)
  flux-schema validate ./manifests --schema-location ./my-schemas

  # Combine the default catalog with a custom local schema layout
  flux-schema validate ./manifests \
    --schema-location default \
    --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'

  # Read manifests from a pipe
  kustomize build . | flux-schema validate /dev/stdin \
    --schema-location default \
    --schema-location ./crd-schemas

  # Skip specific kinds by Kind or apiVersion/Kind
  flux-schema validate ./manifests \
    --skip-kind Secret \
    --skip-kind source.toolkit.fluxcd.io/v1/GitRepository`,
	Args: cobra.MinimumNArgs(1),
	RunE: validateCmdRun,
}

type validateFlags struct {
	schemaLocations    []string
	skipMissingSchemas bool
	skipKinds          []string
	verbose            bool
}

// defaultValidateSchemaLocation points at the flux-schema catalog,
// covering the latest stable Kubernetes and Flux APIs.
// It is used when no --schema-location is provided, and is what
// the literal value "default" expands to in --schema-location.
const defaultValidateSchemaLocation = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/catalog/latest/" + defaultSchemaLayout

// defaultSchemaLayout is the Go-template tail appended to any --schema-location
// value that doesn't already end in .json, so a bare path/URL like
// "./my-schemas" expands to "./my-schemas/{{.Group}}/{{.Kind}}_{{.Version}}.json".
// This matches the per-group directory layout that `flux-schema extract` writes
// by default, so the common case round-trips without a Go template on the flag.
const defaultSchemaLayout = "{{.Group}}/{{.Kind}}_{{.Version}}.json"

var validateArgs = validateFlags{}

const stdinPath = "/dev/stdin"

func init() {
	validateCmd.Flags().StringArrayVar(&validateArgs.schemaLocations, "schema-location", nil,
		"URL or file path for schemas (repeatable); bare paths/URLs get the default layout appended; 'default' points at the built-in catalog")
	validateCmd.Flags().BoolVar(&validateArgs.skipMissingSchemas, "skip-missing-schemas", false,
		"skip documents for which no schema can be found instead of failing")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipKinds, "skip-kind", nil,
		"skip documents matching Kind or apiVersion/Kind (repeatable)")
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
		SkipKinds:          validateArgs.skipKinds,
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

	// Reorder buffer: workers complete documents out of order, but we want
	// deterministic (source, docIndex) output. For each source we track the
	// next expected docIndex and a pending map; results for the source at
	// sourceOrder[currentIdx] flush as soon as their docIndex matches
	// nextIdx. The validator emits a Final sentinel once a source is fully
	// drained, which lets currentIdx advance mid-stream — so all sources
	// stream output in arrival order rather than only the first one.
	type sourceBuf struct {
		nextIdx int
		pending map[int]validator.Result
	}
	bufs := map[string]*sourceBuf{}
	var sourceOrder []string
	completed := map[string]bool{}
	currentIdx := 0

	flushContiguous := func(src string) {
		buf := bufs[src]
		if buf == nil {
			return
		}
		for {
			r, ok := buf.pending[buf.nextIdx]
			if !ok {
				return
			}
			if shouldPrint(r.Status, validateArgs.verbose) {
				writeResult(cmd, r)
			}
			delete(buf.pending, buf.nextIdx)
			buf.nextIdx++
		}
	}

	// flushRemaining drains pending entries past a gap (left by validateDoc
	// skipping content-free YAML) in sorted docIndex order. Only safe to
	// call once a source is known to be fully drained.
	flushRemaining := func(src string) {
		buf := bufs[src]
		if buf == nil || len(buf.pending) == 0 {
			return
		}
		indices := make([]int, 0, len(buf.pending))
		for i := range buf.pending {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		for _, i := range indices {
			r := buf.pending[i]
			if shouldPrint(r.Status, validateArgs.verbose) {
				writeResult(cmd, r)
			}
		}
		buf.pending = nil
	}

	tryAdvance := func() {
		for currentIdx < len(sourceOrder) {
			src := sourceOrder[currentIdx]
			flushContiguous(src)
			if !completed[src] {
				return
			}
			flushRemaining(src)
			currentIdx++
		}
	}

	for r := range v.ValidateSources(ctx, args) {
		if r.Final {
			completed[r.Source] = true
			tryAdvance()
			continue
		}

		files[r.Source] = struct{}{}
		switch r.Status {
		case validator.StatusValid:
			nValid++
		case validator.StatusInvalid:
			nInvalid++
		case validator.StatusSkipped:
			nSkipped++
		}

		if _, ok := bufs[r.Source]; !ok {
			bufs[r.Source] = &sourceBuf{nextIdx: 1, pending: map[int]validator.Result{}}
			sourceOrder = append(sourceOrder, r.Source)
		}
		bufs[r.Source].pending[r.DocIndex] = r
		if currentIdx < len(sourceOrder) && sourceOrder[currentIdx] == r.Source {
			flushContiguous(r.Source)
		}
	}

	// Channel closed: every source we registered should have received a
	// Final sentinel and tryAdvance should already have flushed and advanced
	// past it. Defensive flush for any source missed (e.g. ctx cancellation
	// dropped a sentinel mid-flight).
	for _, src := range sourceOrder[currentIdx:] {
		flushContiguous(src)
		flushRemaining(src)
	}

	writeSummary(cmd, len(files), nValid+nInvalid+nSkipped, nValid, nInvalid, nSkipped, stdinOnly)

	if nInvalid > 0 {
		// Summary line already communicates the failure; exit non-zero
		// via errSilent so we don't print a redundant "✗ ..." line.
		return errSilent
	}
	return nil
}

// expandSchemaLocations normalizes each --schema-location value so callers can
// pass either a full Go template or a bare path/URL:
//
//   - A case-insensitive literal "default" expands to defaultValidateSchemaLocation.
//   - A value ending in ".json" is assumed to already be a complete template and
//     is taken verbatim.
//   - Anything else has defaultSchemaLayout appended under a single "/", so
//     "./my-schemas" becomes "./my-schemas/{{.Group}}/{{.Kind}}_{{.Version}}.json".
//     For URLs, the tail is spliced before any "?query" or "#fragment" so the
//     template lands on the path, not inside the query string.
//
// Order is preserved so the user controls fallback priority.
//
// Note: when no --schema-location is passed the caller uses
// defaultValidateSchemaLocation directly (see validateCmdRun) and does not go
// through this function — that default is already a complete template.
func expandSchemaLocations(locations []string) ([]string, error) {
	out := make([]string, len(locations))
	for i, loc := range locations {
		if strings.TrimSpace(loc) == "" {
			return nil, fmt.Errorf("--schema-location must not be empty")
		}
		if strings.EqualFold(loc, "default") {
			out[i] = defaultValidateSchemaLocation
			continue
		}
		if !strings.HasSuffix(loc, ".json") {
			loc = appendSchemaLayout(loc)
		}
		out[i] = loc
	}
	return out, nil
}

// appendSchemaLayout appends defaultSchemaLayout to loc, preserving any URL
// query string or fragment. "./schemas" → "./schemas/<layout>";
// "https://host/catalog?ref=main" → "https://host/catalog/<layout>?ref=main".
// Trailing "/" and "\" are both stripped so a Windows path like ".\schemas\"
// normalizes cleanly — Go's filepath layer accepts forward slashes on Windows.
func appendSchemaLayout(loc string) string {
	base, tail := loc, ""
	if i := strings.IndexAny(loc, "?#"); i >= 0 {
		base, tail = loc[:i], loc[i:]
	}
	return strings.TrimRight(base, `/\`) + "/" + defaultSchemaLayout + tail
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
