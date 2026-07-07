// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/flags"
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

  # Read manifests from a pipe and add CNCF project CRDs from the ecosystem catalog
  kustomize build . | flux-schema validate \
    --schema-location default \
    --schema-location ./crd-schemas \
    --schema-location ecosystem

  # Skip specific kinds by Kind or apiVersion/Kind
  flux-schema validate ./manifests \
    --skip-kind Service \
    --skip-kind source.toolkit.fluxcd.io/v1/GitRepository

  # Strip non-conformant fields before validation
  # e.g. SOPS metadata that Flux removes at apply time
  flux-schema validate ./manifests \
    --skip-json-path v1/Secret:/sops

  # Skip files and directories by basename glob
  # default skips dotfiles and dot-directories e.g. '.git'
  flux-schema validate ./manifests \
    --skip-file '.*' \
    --skip-file 'kustomization.yaml'

  # Load flag defaults from a checked-in YAML config (CLI flags still override)
  flux-schema validate ./manifests --config .fluxschema.yml`,
	RunE: validateCmdRun,
}

type validateFlags struct {
	schemaLocations       []string
	skipMissingSchemas    bool
	skipKinds             []string
	skipJSONPaths         []string
	skipFiles             []string
	skipCELRules          bool
	verbose               bool
	failFast              bool
	concurrent            int
	insecureSkipTLSVerify bool
	configFile            string
	output                flags.Output
}

var validateArgs = validateFlags{
	concurrent: validator.DefaultWorkers,
	output:     "text",
}

func init() {
	validateCmd.Flags().StringArrayVar(&validateArgs.schemaLocations, "schema-location", nil,
		"URL or file path for schemas (repeatable); 'default' points at the built-in catalog, 'ecosystem' at schemas.fluxoperator.dev")
	validateCmd.Flags().BoolVar(&validateArgs.skipMissingSchemas, "skip-missing-schemas", false,
		"skip documents for which no schema can be found instead of failing")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipKinds, "skip-kind", nil,
		"skip documents matching kind or apiVersion/kind e.g. 'v1/Secret' (repeatable)")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipJSONPaths, "skip-json-path", nil,
		"strip a JSON Pointer field, optionally scoped e.g. 'v1/Secret:/sops' (repeatable)")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipFiles, "skip-file", nil,
		"glob pattern matched against files and dirs "+
			"defaults to skipping dotfiles and dot-dirs (repeatable)")
	validateCmd.Flags().BoolVar(&validateArgs.skipCELRules, "skip-cel-rules", false,
		"skip evaluation of x-kubernetes-validations CEL rules")
	validateCmd.Flags().BoolVarP(&validateArgs.verbose, "verbose", "v", false,
		"print a line for every document, including valid and skipped")
	validateCmd.Flags().BoolVar(&validateArgs.failFast, "fail-fast", false,
		"exit after the first invalid document")
	validateCmd.Flags().IntVar(&validateArgs.concurrent, "concurrent", validator.DefaultWorkers,
		"number of concurrent workers")
	validateCmd.Flags().BoolVar(&validateArgs.insecureSkipTLSVerify, "insecure-skip-tls-verify", false,
		"disable TLS certificate verification when fetching schemas over HTTPS")
	validateCmd.Flags().StringVar(&validateArgs.configFile, "config", "",
		"path to a YAML file supplying default values for validate flags "+
			"(env: "+envConfigFile+", default: <executable>.config)")
	_ = validateCmd.MarkFlagFilename("config", "yaml", "yml")
	validateCmd.Flags().VarP(&validateArgs.output, "output", "o", validateArgs.output.Description())
	rootCmd.AddCommand(validateCmd)
}

func validateCmdRun(cmd *cobra.Command, args []string) error {
	if err := loadValidateConfig(cmd); err != nil {
		return err
	}

	inputs, err := resolveStdinArgs(args)
	if err != nil {
		return err
	}

	opts, err := buildValidatorOptions(inputs)
	if err != nil {
		return err
	}
	v, err := validator.New(opts)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	stdinOnly := len(inputs) == 1 && inputs[0] == stdinLabel
	mode := validateArgs.output.String()

	files := make(map[string]struct{})
	var nValid, nInvalid, nSkipped int
	var collected []validator.Result

	// Text mode streams per-result; structured modes buffer so the envelope
	// can carry the full summary ahead of results[].
	emit := func(r validator.Result) {
		if mode == "text" {
			if shouldPrint(r.Status, validateArgs.verbose) {
				writeResult(cmd, r)
			}
			return
		}
		collected = append(collected, r)
	}

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
			emit(r)
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
		slices.Sort(indices)
		for _, i := range indices {
			emit(buf.pending[i])
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

	if mode != "text" {
		summary := apiv1.ReportSummary{
			Total:   nValid + nInvalid + nSkipped,
			Valid:   nValid,
			Invalid: nInvalid,
			Skipped: nSkipped,
		}
		report := validator.NewReport("flux-schema/"+VERSION, time.Now(), collected, summary)
		if err := writeReport(cmd, mode, report); err != nil {
			return err
		}
	} else {
		writeSummary(cmd, len(files), nValid+nInvalid+nSkipped, nValid, nInvalid, nSkipped, stdinOnly)
	}

	if nInvalid > 0 {
		// Summary line already communicates the failure; exit non-zero
		// via errSilent so we don't print a redundant "✗ ..." line.
		return errSilent
	}
	return nil
}

// writeReport streams report to cmd's output as JSON or YAML. The `$schema`
// key is JSON-only: it points at a JSON Schema document and carries no
// meaning for YAML consumers, so we drop it in YAML mode.
func writeReport(cmd *cobra.Command, mode string, report apiv1.Report) error {
	switch mode {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		return nil
	case "yaml":
		report.Schema = ""
		data, err := yaml.Marshal(report)
		if err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		cmd.Print(string(data))
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", mode)
	}
}

// expandSchemaLocations normalizes each --schema-location value so callers can
// pass either a full Go template or a bare path/URL:
//
//   - A case-insensitive literal "default" expands to validator.DefaultSchemaLocation.
//   - A case-insensitive literal "ecosystem" expands to validator.EcosystemSchemaLocation.
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
		if strings.EqualFold(loc, "ecosystem") {
			out[i] = validator.EcosystemSchemaLocation
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
	if r.Reason != validator.ReasonNone {
		cmd.Printf("%s - %s %s: %s\n", r.Source, r.Identifier(), verb, r.Reason)
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

// loadValidateConfig applies a config file resolved from --config,
// FLUX_SCHEMA_CONFIG, or the executable-adjacent default path. A missing
// default config is a no-op.
func loadValidateConfig(cmd *cobra.Command) error {
	configPath, ok, err := resolveConfigFile(validateArgs.configFile)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	cfg, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}
	return applyValidateConfig(cmd, &cfg.Validate, &validateArgs)
}

// buildValidatorOptions expands --schema-location values, validates flag
// invariants, and assembles the validator.Options. Stdin is wired in when
// inputs references the stdin sentinel.
func buildValidatorOptions(inputs []string) (validator.Options, error) {
	locations := validateArgs.schemaLocations
	if len(locations) == 0 {
		locations = []string{validator.DefaultSchemaLocation}
	} else {
		expanded, err := expandSchemaLocations(locations)
		if err != nil {
			return validator.Options{}, err
		}
		locations = expanded
	}
	if validateArgs.concurrent < 1 {
		return validator.Options{}, fmt.Errorf("--concurrent must be >= 1, got %d", validateArgs.concurrent)
	}
	opts := validator.Options{
		SchemaLocations:       locations,
		SkipMissingSchemas:    validateArgs.skipMissingSchemas,
		SkipKinds:             validateArgs.skipKinds,
		SkipJSONPaths:         validateArgs.skipJSONPaths,
		SkipFiles:             validateArgs.skipFiles,
		SkipCELRules:          validateArgs.skipCELRules,
		HTTPTimeout:           rootArgs.timeout,
		Workers:               validateArgs.concurrent,
		InsecureSkipTLSVerify: validateArgs.insecureSkipTLSVerify,
	}
	if slices.Contains(inputs, stdinLabel) {
		opts.Stdin = stdinReader
	}
	return opts, nil
}
