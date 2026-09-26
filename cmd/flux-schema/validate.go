// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/fluxcd/flux-schema/internal/junitxml"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/flag"
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

  # Skip required-field errors for values defaulted by admission hooks
  flux-schema validate ./manifests \
    --skip-json-path-if-absent Widget:/spec/replicas

  # Skip files and directories by basename glob
  # default skips dotfiles and dot-directories e.g. '.git'
  flux-schema validate ./manifests \
    --skip-file '.*' \
    --skip-file 'kustomization.yaml'

  # Load flag defaults from a checked-in YAML config (CLI flags still override)
  flux-schema validate ./manifests --config .fluxschema.yml`,
	RunE: validateCmdRun,
}

var validateOutputs = []string{"text", "yaml", "json", "junit"}

type validateFlags struct {
	schemaLocations       []string
	skipMissingSchemas    bool
	skipKinds             []string
	skipJSONPaths         []string
	skipJSONPathIfAbsent  []string
	skipFiles             []string
	skipCELRules          bool
	verbose               bool
	failFast              bool
	concurrent            int
	insecureSkipTLSVerify bool
	configFile            string
	output                flag.Output
	outputDir             string
}

var validateArgs = validateFlags{
	concurrent: validator.DefaultWorkers,
	output:     "text",
}

func init() {
	outputValue := flag.NewOutputValue(&validateArgs.output, validateOutputs...)

	validateCmd.Flags().StringArrayVarP(&validateArgs.schemaLocations, "schema-location", "s", nil,
		"URL or file path for schemas (repeatable); 'default' points at the built-in catalog, 'ecosystem' at schemas.fluxoperator.dev")
	validateCmd.Flags().BoolVar(&validateArgs.skipMissingSchemas, "skip-missing-schemas", false,
		"skip documents for which no schema can be found instead of failing")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipKinds, "skip-kind", nil,
		"skip documents matching kind or apiVersion/kind e.g. 'v1/Secret' (repeatable)")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipJSONPaths, "skip-json-path", nil,
		"strip a JSON Pointer field, optionally scoped e.g. 'v1/Secret:/sops' (repeatable)")
	validateCmd.Flags().StringArrayVar(&validateArgs.skipJSONPathIfAbsent, "skip-json-path-if-absent", nil,
		"skip a missing required JSON Pointer field, optionally scoped e.g. 'Widget:/spec/replicas' (repeatable)")
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
	validateCmd.Flags().VarP(outputValue, "output", "o", outputValue.Description())
	validateCmd.Flags().StringVarP(&validateArgs.outputDir, "output-dir", "d", "",
		"directory where structured report files are written (created if missing)")
	rootCmd.AddCommand(validateCmd)
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

type resultCollector struct {
	outputWriter

	nValid   int
	nInvalid int
	nSkipped int

	bufs        map[string]*sourceBuf
	sourceOrder []string
	completed   map[string]bool
	currentIdx  int
}

func newResultCollector(w outputWriter) *resultCollector {
	return &resultCollector{
		outputWriter: w,
		bufs:         make(map[string]*sourceBuf),
		completed:    make(map[string]bool),
	}
}

func (c *resultCollector) add(r validator.Result) {
	if r.Final {
		c.completed[r.Source] = true
		c.tryAdvance()
		return
	}

	switch r.Status {
	case validator.StatusValid:
		c.nValid++
	case validator.StatusInvalid:
		c.nInvalid++
	case validator.StatusSkipped:
		c.nSkipped++
	}

	buf, ok := c.bufs[r.Source]
	if !ok {
		buf = &sourceBuf{
			nextIdx: 1,
			pending: map[int]validator.Result{},
		}
		c.bufs[r.Source] = buf
		c.sourceOrder = append(c.sourceOrder, r.Source)
	}

	buf.pending[r.DocIndex] = r

	if c.currentIdx < len(c.sourceOrder) &&
		c.sourceOrder[c.currentIdx] == r.Source {
		c.flushContiguous(r.Source)
	}
}

func (c *resultCollector) flushContiguous(src string) {
	buf := c.bufs[src]
	if buf == nil {
		return
	}

	for {
		r, ok := buf.pending[buf.nextIdx]
		if !ok {
			return
		}

		c.WriteResult(r)
		delete(buf.pending, buf.nextIdx)
		buf.nextIdx++
	}
}

// flushRemaining drains pending entries past a gap (left by validateDoc
// skipping content-free YAML) in sorted docIndex order. Only safe to
// call once a source is known to be fully drained.
func (c *resultCollector) flushRemaining(src string) {
	buf := c.bufs[src]
	if buf == nil || len(buf.pending) == 0 {
		return
	}

	indices := make([]int, 0, len(buf.pending))
	for i := range buf.pending {
		indices = append(indices, i)
	}
	slices.Sort(indices)

	for _, i := range indices {
		c.WriteResult(buf.pending[i])
	}

	buf.pending = nil
}

func (c *resultCollector) tryAdvance() {
	for c.currentIdx < len(c.sourceOrder) {
		src := c.sourceOrder[c.currentIdx]

		c.flushContiguous(src)
		if !c.completed[src] {
			return
		}

		c.flushRemaining(src)
		c.currentIdx++
	}
}

func (c *resultCollector) flushRemainingSources() {
	// Channel closed: every source we registered should have received a
	// Final sentinel and tryAdvance should already have flushed and advanced
	// past it. Defensive flush for any source missed (e.g. ctx cancellation
	// dropped a sentinel mid-flight).
	for _, src := range c.sourceOrder[c.currentIdx:] {
		c.flushContiguous(src)
		c.flushRemaining(src)
	}
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

	var writer outputWriter
	switch mode {
	case "text":
		if validateArgs.outputDir != "" {
			return fmt.Errorf("output directory is currently not supported for text output")
		}
		writer = &textWriter{cmd: cmd, verbose: validateArgs.verbose}
	default:
		if validateArgs.outputDir != "" {
			destDir := validateArgs.outputDir
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}

			fileExt := mode
			// Special case for JUnit XML reports for now.
			// Consider functional constants
			if mode == "junit" {
				fileExt = "xml"
			}
			// The validate action script will currently invoke the validate command multiple times.
			// So in order to support the output-dir flag when used with the script or GitHub Action,
			// it is required to emit unique filenames.
			// Ideally, one execution of the validate script should produce one report,
			// which will make it possible to have a deterministic file name.
			// But for now we have to use a random or hashed name.
			f, err := os.CreateTemp(destDir, "flux-validate-*."+fileExt)
			if err != nil {
				return fmt.Errorf("create report file: %w", err)
			}
			defer f.Close()
			if err := f.Chmod(0o644); err != nil {
				return fmt.Errorf("change report file mode: %w", err)
			}

			writer = &multiOutputWriter{
				writers: []outputWriter{
					&reportWriter{writer: f, mode: mode},
					&textWriter{cmd: cmd, verbose: validateArgs.verbose},
				},
			}
		} else {
			writer = &reportWriter{writer: cmd.OutOrStdout(), mode: mode}
		}
	}

	collector := newResultCollector(writer)

	for r := range v.ValidateSources(ctx, inputs) {
		collector.add(r)

		// Fail-fast cancels mid-stream; the defensive flush below still prints
		// any buffered invalid even when an earlier source lost its Final.
		if validateArgs.failFast && r.Status == validator.StatusInvalid {
			cancel()
		}
	}

	collector.flushRemainingSources()

	summary := apiv1.ReportSummary{
		Total:   collector.nValid + collector.nInvalid + collector.nSkipped,
		Valid:   collector.nValid,
		Invalid: collector.nInvalid,
		Skipped: collector.nSkipped,
	}
	err = writer.WriteSummary(summary, len(collector.bufs), stdinOnly)
	if err != nil {
		return err
	}

	if collector.nInvalid > 0 {
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
		SkipJSONPathIfAbsent:  validateArgs.skipJSONPathIfAbsent,
		SkipFiles:             validateArgs.skipFiles,
		SkipCELRules:          validateArgs.skipCELRules,
		UserAgent:             userAgent(),
		HTTPTimeout:           rootArgs.timeout,
		Workers:               validateArgs.concurrent,
		InsecureSkipTLSVerify: validateArgs.insecureSkipTLSVerify,
	}
	if slices.Contains(inputs, stdinLabel) {
		opts.Stdin = stdinReader
	}
	return opts, nil
}

type outputWriter interface {
	WriteResult(validator.Result)
	WriteSummary(summary apiv1.ReportSummary, nFiles int, stdinOnly bool) error
}

type textWriter struct {
	cmd     *cobra.Command
	verbose bool
}

func (w *textWriter) WriteResult(r validator.Result) {
	if !shouldPrint(r.Status, w.verbose) {
		return
	}

	verb := "is invalid"
	switch r.Status {
	case validator.StatusValid:
		verb = "is valid"
	case validator.StatusSkipped:
		verb = "is skipped"
	}
	if r.Reason != validator.ReasonNone {
		w.cmd.Printf("%s - %s %s: %s\n", r.Source, r.Identifier(), verb, r.Reason)
	} else {
		w.cmd.Printf("%s - %s %s\n", r.Source, r.Identifier(), verb)
	}
	for _, e := range r.Errors {
		if e.Path == "" {
			w.cmd.Printf("  - %s\n", e.Msg)
		} else {
			w.cmd.Printf("  - %s: %s\n", e.Path, e.Msg)
		}
	}
}

func (w *textWriter) WriteSummary(s apiv1.ReportSummary, nFiles int, stdinOnly bool) error {
	resources := pluralize("resource", s.Total)
	if stdinOnly {
		w.cmd.Printf("Summary: %d %s found parsing stdin - Valid: %d, Invalid: %d, Skipped: %d\n",
			s.Total, resources, s.Valid, s.Invalid, s.Skipped)
		return nil
	}
	files := pluralize("file", nFiles)
	w.cmd.Printf("Summary: %d %s found in %d %s - Valid: %d, Invalid: %d, Skipped: %d\n",
		s.Total, resources, nFiles, files, s.Valid, s.Invalid, s.Skipped)
	return nil
}

type reportWriter struct {
	writer io.Writer
	mode   string

	collected []validator.Result
}

func (w *reportWriter) WriteResult(r validator.Result) {
	// Structured modes buffer so the envelope can carry the full summary ahead of results[].
	w.collected = append(w.collected, r)
}

func (w *reportWriter) WriteSummary(s apiv1.ReportSummary, _ int, _ bool) error {
	report := validator.NewReport(
		"flux-schema/"+VERSION,
		time.Now(),
		w.collected,
		s,
	)

	switch w.mode {
	case "json":
		enc := json.NewEncoder(w.writer)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		return nil
	case "yaml":
		// The `$schema` key is JSON-only: it points at a JSON Schema document
		// and carries no meaning for YAML consumers, so we drop it in YAML mode.
		report.Schema = ""
		data, err := yaml.Marshal(report)
		if err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		_, err = w.writer.Write(data)
		if err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		return nil
	case "junit":
		testSuites := junitxml.FromReport(report)
		if err := testSuites.Write(w.writer); err != nil {
			return fmt.Errorf("write JUnitXML report: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", w.mode)
	}
}

type multiOutputWriter struct {
	writers []outputWriter
}

func (mw multiOutputWriter) WriteResult(r validator.Result) {
	for _, w := range mw.writers {
		w.WriteResult(r)
	}
}

func (mw multiOutputWriter) WriteSummary(summary apiv1.ReportSummary, nFiles int, stdinOnly bool) error {
	for _, w := range mw.writers {
		if err := w.WriteSummary(summary, nFiles, stdinOnly); err != nil {
			return err
		}
	}
	return nil
}
