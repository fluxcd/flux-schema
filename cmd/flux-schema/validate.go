// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"slices"
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
  # https://github.com/fluxcd/flux-schema/blob/main/catalog/README.md
  flux-schema validate ./manifests --verbose

  # Validate against a local schema directory written by 'flux-schema extract'
  # (bare paths/URLs get '{{.Group}}/{{.Kind}}_{{.Version}}.json' appended)
  flux-schema validate ./manifests --schema-location ./my-schemas

  # Combine the default catalog with a custom local schema layout
  flux-schema validate ./manifests \
    --schema-location default \
    --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'

  # Read manifests from a pipe and add remote schema fallback
  kustomize build . | flux-schema validate \
    --schema-location default \
    --schema-location ./crd-schemas \
    --schema-location https://raw.githubusercontent.com/datreeio/CRDs-catalog/main

  # Skip specific kinds by Kind or apiVersion/Kind
  flux-schema validate ./manifests \
    --skip-kind Secret \
    --skip-kind source.toolkit.fluxcd.io/v1/GitRepository`,
	RunE: validateCmdRun,
}

type validateFlags struct {
	schemaLocations       []string
	skipMissingSchemas    bool
	skipKinds             []string
	verbose               bool
	failFast              bool
	concurrent            int
	insecureSkipTLSVerify bool
}

var validateArgs = validateFlags{
	concurrent: validator.DefaultWorkers,
}

func init() {
	validateCmd.Flags().StringArrayVar(&validateArgs.schemaLocations, "schema-location", nil,
		"URL or file path for schemas (repeatable); 'default' points at the built-in catalog")
	validateCmd.Flags().BoolVar(&validateArgs.skipMissingSchemas, "skip-missing-schemas", false,
		"skip documents for which no schema can be found instead of failing")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipKinds, "skip-kind", nil,
		"skip documents matching Kind or apiVersion/Kind (repeatable)")
	validateCmd.Flags().BoolVarP(&validateArgs.verbose, "verbose", "v", false,
		"print a line for every document, including valid and skipped")
	validateCmd.Flags().BoolVar(&validateArgs.failFast, "fail-fast", false,
		"exit after the first invalid document")
	validateCmd.Flags().IntVar(&validateArgs.concurrent, "concurrent", validator.DefaultWorkers,
		"number of concurrent workers")
	validateCmd.Flags().BoolVar(&validateArgs.insecureSkipTLSVerify, "insecure-skip-tls-verify", false,
		"disable TLS certificate verification when fetching schemas over HTTPS")
	rootCmd.AddCommand(validateCmd)
}

func validateCmdRun(cmd *cobra.Command, args []string) error {
	inputs, err := resolveStdinArgs(args)
	if err != nil {
		return err
	}

	locations := validateArgs.schemaLocations
	if len(locations) == 0 {
		locations = []string{validator.DefaultSchemaLocation}
	} else {
		expanded, lerr := expandSchemaLocations(locations)
		if lerr != nil {
			return lerr
		}
		locations = expanded
	}

	if validateArgs.concurrent < 1 {
		return fmt.Errorf("--concurrent must be >= 1, got %d", validateArgs.concurrent)
	}

	opts := validator.Options{
		SchemaLocations:       locations,
		SkipMissingSchemas:    validateArgs.skipMissingSchemas,
		SkipKinds:             validateArgs.skipKinds,
		HTTPTimeout:           rootArgs.timeout,
		Workers:               validateArgs.concurrent,
		InsecureSkipTLSVerify: validateArgs.insecureSkipTLSVerify,
	}
	if slices.Contains(inputs, stdinLabel) {
		opts.Stdin = stdinReader
	}
	v, err := validator.New(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	stdinOnly := len(inputs) == 1 && inputs[0] == stdinLabel

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

	for r := range v.ValidateSources(ctx, inputs) {
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

		// Fail-fast cancels mid-stream; the defensive flush below still prints
		// any buffered invalid even when an earlier source lost its Final.
		if validateArgs.failFast && r.Status == validator.StatusInvalid {
			cancel()
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
//   - A case-insensitive literal "default" expands to validator.DefaultSchemaLocation.
//   - A value ending in ".json" is assumed to already be a complete template and
//     is taken verbatim.
//   - Anything else has validator.DefaultSchemaLayout appended under a single "/", so
//     "./my-schemas" becomes "./my-schemas/{{.Group}}/{{.Kind}}_{{.Version}}.json".
//     For URLs, the tail is spliced before any "?query" or "#fragment" so the
//     template lands on the path, not inside the query string.
//
// Order is preserved so the user controls fallback priority.
//
// Note: when no --schema-location is passed the caller uses
// validator.DefaultSchemaLocation directly (see validateCmdRun) and does not go
// through this function — that default is already a complete template.
func expandSchemaLocations(locations []string) ([]string, error) {
	out := make([]string, len(locations))
	for i, loc := range locations {
		if strings.TrimSpace(loc) == "" {
			return nil, fmt.Errorf("--schema-location must not be empty")
		}
		if strings.EqualFold(loc, "default") {
			out[i] = validator.DefaultSchemaLocation
			continue
		}
		if !strings.HasSuffix(loc, ".json") {
			loc = appendSchemaLayout(loc)
		}
		out[i] = loc
	}
	return out, nil
}

// appendSchemaLayout appends validator.DefaultSchemaLayout to loc, preserving any URL
// query string or fragment. "./schemas" → "./schemas/<layout>";
// "https://host/catalog?ref=main" → "https://host/catalog/<layout>?ref=main".
// Trailing "/" and "\" are both stripped so a Windows path like ".\schemas\"
// normalizes cleanly — Go's filepath layer accepts forward slashes on Windows.
func appendSchemaLayout(loc string) string {
	base, tail := loc, ""
	if i := strings.IndexAny(loc, "?#"); i >= 0 {
		base, tail = loc[:i], loc[i:]
	}
	return strings.TrimRight(base, `/\`) + "/" + validator.DefaultSchemaLayout + tail
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
