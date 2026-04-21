// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package validator validates Kubernetes YAML manifests against JSON
// Schemas resolved from one or more --schema-location templates.
//
// Validation is strict by default: YAML duplicate keys are rejected,
// documents missing both metadata.name and metadata.generateName are
// flagged (kube-apiserver admission rule), and schemas produced by
// flux-schema extract enforce additionalProperties: false.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux-schema/internal/tmpl"
	"github.com/fluxcd/flux-schema/internal/yamldoc"
)

const (
	defaultWorkers = 8

	extYAML = ".yaml"
	extYML  = ".yml"
)

// Status is the per-document validation outcome.
type Status int

const (
	StatusValid Status = iota
	StatusInvalid
	StatusSkipped
)

// String renders the status as it appears in CLI output.
func (s Status) String() string {
	switch s {
	case StatusValid:
		return "valid"
	case StatusInvalid:
		return "invalid"
	case StatusSkipped:
		return "skipped"
	}
	return "unknown"
}

// Result is the outcome of validating a single YAML document.
type Result struct {
	Source     string
	DocIndex   int
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Status     Status
	Message    string
	Errors     []ValidationError
}

// ValidationError is one per-field JSON Schema violation.
type ValidationError struct {
	Path string
	Msg  string
}

// Identifier returns the Flux-canonical object reference used in CLI output:
// "Kind/Namespace/Name" for namespaced resources, "Kind/Name" otherwise.
func (r Result) Identifier() string {
	if r.Namespace != "" {
		return r.Kind + "/" + r.Namespace + "/" + r.Name
	}
	return r.Kind + "/" + r.Name
}

// Options configures a Validator.
type Options struct {
	SchemaLocations    []string
	SkipMissingSchemas bool
	HTTPClient         *retryablehttp.Client
	HTTPTimeout        time.Duration
	Workers            int
}

// Validator resolves and applies JSON Schemas to Kubernetes manifests.
// It is safe for concurrent use by multiple goroutines.
type Validator struct {
	opts   Options
	loader *SchemaLoader
}

// New returns a Validator configured from opts. Each location template is
// parsed up-front so syntax errors surface before the first document.
func New(opts Options) (*Validator, error) {
	if len(opts.SchemaLocations) == 0 {
		return nil, errors.New("no schema location defined")
	}
	templates := make([]*template.Template, len(opts.SchemaLocations))
	probe := tmpl.SchemaVars{Group: "x", Kind: "x", Version: "x"}
	for i, loc := range opts.SchemaLocations {
		tpl, err := tmpl.Parse(loc)
		if err != nil {
			return nil, fmt.Errorf("schema location %q: %w", loc, err)
		}
		// Dry-run the template with a probe so references to unknown
		// fields fail at construction time rather than on the first doc.
		if _, err := tmpl.Execute(tpl, probe); err != nil {
			return nil, fmt.Errorf("schema location %q: %w", loc, err)
		}
		templates[i] = tpl
	}
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers
	}
	if opts.HTTPClient == nil {
		c := retryablehttp.NewClient()
		c.Logger = nil
		opts.HTTPClient = c
	}

	return &Validator{
		opts:   opts,
		loader: NewSchemaLoader(templates, opts.HTTPClient, opts.HTTPTimeout),
	}, nil
}

type job struct {
	source   string
	docIndex int
	raw      []byte
	// loadErr surfaces a source-level read/open failure so the worker
	// pool reports one Result per failed source instead of silently
	// dropping it.
	loadErr error
}

// ValidateSources streams validation results for every document found in
// paths. Files are walked sequentially by a single producer goroutine;
// documents are validated by a pool of opts.Workers goroutines. The
// returned channel is closed once all documents have been processed.
func (v *Validator) ValidateSources(ctx context.Context, paths []string) <-chan Result {
	results := make(chan Result, v.opts.Workers*2)
	jobs := make(chan job, v.opts.Workers*2)

	var wg sync.WaitGroup
	for i := 0; i < v.opts.Workers; i++ {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					var r Result
					if j.loadErr != nil {
						r = Result{
							Source:   j.source,
							DocIndex: j.docIndex,
							Status:   StatusInvalid,
							Message:  j.loadErr.Error(),
						}
					} else {
						result, emit := v.validateDoc(ctx, j.source, j.docIndex, j.raw)
						if !emit {
							continue
						}
						r = result
					}
					select {
					case results <- r:
					case <-ctx.Done():
						return
					}
				}
			}
		})
	}

	go func() {
		defer close(jobs)
		for _, path := range paths {
			if err := v.produceFromPath(ctx, path, jobs); err != nil {
				select {
				case jobs <- job{source: path, loadErr: err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// ValidateBytes validates an in-memory YAML payload sequentially. Primarily
// used by tests and by callers that have already read from stdin.
func (v *Validator) ValidateBytes(ctx context.Context, source string, data []byte) []Result {
	var out []Result
	scanner := yamldoc.NewScanner(bytes.NewReader(data))
	idx := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if isContentFree(raw) {
			continue
		}
		idx++
		buf := make([]byte, len(raw))
		copy(buf, raw)
		if result, emit := v.validateDoc(ctx, source, idx, buf); emit {
			out = append(out, result)
		}
	}
	return out
}

// produceFromPath opens path (or walks it, if a directory) and streams each
// YAML document into jobs. Returns a source-level error only for paths that
// cannot be stat'd or opened; per-document failures are reported via
// validateDoc inside the worker pool.
func (v *Validator) produceFromPath(ctx context.Context, path string, jobs chan<- job) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				if p != path && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != extYAML && ext != extYML {
				return nil
			}
			return v.streamFile(ctx, p, jobs)
		})
	}
	return v.streamFile(ctx, path, jobs)
}

func (v *Validator) streamFile(ctx context.Context, path string, jobs chan<- job) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := yamldoc.NewScanner(f)
	idx := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if isContentFree(raw) {
			continue
		}
		idx++
		buf := make([]byte, len(raw))
		copy(buf, raw)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- job{source: path, docIndex: idx, raw: buf}:
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

// validateDoc runs the full per-document pipeline: strict YAML decode,
// apiVersion/kind extraction, name-or-generateName admission check, schema
// resolution, and JSON Schema validation.
//
// Returns emit=false when the document contains only comments or whitespace
// (YAML decodes to nil); such content-free documents are dropped entirely
// rather than surfaced as skipped, because they are not "resources" the user
// intended to validate — a file starting with `# header\n---\n...` is idiomatic.
func (v *Validator) validateDoc(ctx context.Context, source string, idx int, raw []byte) (Result, bool) {
	r := Result{Source: source, DocIndex: idx}
	settle := func(s Status, msg string) (Result, bool) {
		r.Status = s
		r.Message = msg
		return r, true
	}
	// missingSchemaStatus picks between StatusSkipped and StatusInvalid for
	// "we can't resolve a schema for this document" cases, honoring the
	// --skip-missing-schemas flag.
	missingSchemaStatus := func() Status {
		if v.opts.SkipMissingSchemas {
			return StatusSkipped
		}
		return StatusInvalid
	}

	var doc map[string]any
	if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
		// Lenient re-parse so the CLI line can still show Kind/Namespace/Name
		// for a doc that failed admission (e.g. duplicate keys), rather than
		// the anonymous "/#1".
		var lenient map[string]any
		if yaml.Unmarshal(raw, &lenient) == nil && lenient != nil {
			r.APIVersion, r.Kind, r.Namespace, r.Name = extractIdentity(lenient)
		}
		if r.Name == "" {
			r.Name = fmt.Sprintf("#%d", idx)
		}
		r.Errors = splitYAMLError(err)
		return settle(StatusInvalid, "YAML parse failed")
	}
	if doc == nil {
		// Content-free document (comments or whitespace only). Idiomatic
		// at the top of files with a header block before the first `---`.
		return Result{}, false
	}

	r.APIVersion, r.Kind, r.Namespace, _ = extractIdentity(doc)
	metadata, _ := doc["metadata"].(map[string]any)
	name, hasIdentity := computeName(metadata, idx)
	r.Name = name

	if r.APIVersion == "" || r.Kind == "" {
		return settle(missingSchemaStatus(), "document missing apiVersion/kind")
	}

	if !hasIdentity {
		r.Errors = []ValidationError{{
			Path: "/metadata",
			Msg:  "missing property 'name' or 'generateName'",
		}}
		return settle(StatusInvalid, "validation failed")
	}

	group, version, ok := strings.Cut(r.APIVersion, "/")
	if !ok {
		// core group: apiVersion is just "v1".
		group, version = "", group
	}
	vars := tmpl.SchemaVars{Group: group, Kind: r.Kind, Version: version}

	schema, _, found, err := v.loader.Resolve(ctx, vars)
	if err != nil {
		r.Errors = []ValidationError{{Msg: err.Error()}}
		return settle(StatusInvalid, "schema resolution failed")
	}
	if !found {
		r.Errors = []ValidationError{{
			Msg: fmt.Sprintf("no schema for kind %q in version %q", r.Kind, r.APIVersion),
		}}
		return settle(missingSchemaStatus(), "schema not found")
	}

	if err := schema.Validate(doc); err != nil {
		r.Errors = flattenErrors(err)
		return settle(StatusInvalid, "schema validation failed")
	}
	r.Status = StatusValid
	return r, true
}

// splitYAMLError turns a YAML decode error into clean, per-violation details
// the CLI can render on indented sub-lines, the same way JSON Schema
// violations are rendered.
//
// YAML decode errors arrive with noisy prefixes and, for strict-mode
// failures like duplicate keys, several violations packed into one
// multi-line string. Printed as-is they would spill past the invalid
// line and drown the surrounding output. splitYAMLError peels off the
// prefixes and returns one entry per underlying `line N: ...` message,
// so the caller sees just the parts worth showing the user. Anything
// that doesn't match a known shape is returned verbatim rather than
// dropped.
func splitYAMLError(err error) []ValidationError {
	const (
		multiPrefix  = "error converting YAML to JSON: yaml: unmarshal errors:"
		singlePrefix = "error converting YAML to JSON: yaml: "
		rawPrefix    = "yaml: "
	)
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, multiPrefix); ok {
		var out []ValidationError
		for line := range strings.SplitSeq(rest, "\n") {
			t := strings.TrimSpace(line)
			if t != "" {
				out = append(out, ValidationError{Msg: t})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	msg = strings.TrimPrefix(msg, singlePrefix)
	msg = strings.TrimPrefix(msg, rawPrefix)
	return []ValidationError{{Msg: strings.TrimSpace(msg)}}
}

// extractIdentity returns the apiVersion/kind/namespace/name four-tuple from
// a parsed YAML document. Missing or non-string fields yield empty strings.
func extractIdentity(doc map[string]any) (apiVersion, kind, namespace, name string) {
	apiVersion, _ = doc["apiVersion"].(string)
	kind, _ = doc["kind"].(string)
	if md, _ := doc["metadata"].(map[string]any); md != nil {
		namespace, _ = md["namespace"].(string)
		name, _ = md["name"].(string)
	}
	return
}

// isContentFree reports whether raw is empty or contains only YAML comment
// lines. Dropping these before docIndex is assigned keeps user-visible
// numbering aligned with real documents — a file with a `# header\n---\n`
// preamble should still show its first real resource as doc #1.
func isContentFree(raw []byte) bool {
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] != '#' {
			return false
		}
	}
	return true
}

// computeName derives the document identity per the Kubernetes admission
// rule: metadata.name wins, falling back to "{generateName}{{ generateName }}"
// if set, otherwise "#{docIndex}" paired with hasIdentity=false so the
// caller emits StatusInvalid.
func computeName(metadata map[string]any, docIndex int) (string, bool) {
	name, _ := metadata["name"].(string)
	if name != "" {
		return name, true
	}
	generateName, _ := metadata["generateName"].(string)
	if generateName != "" {
		return generateName + "{{ generateName }}", true
	}
	return fmt.Sprintf("#%d", docIndex), false
}

// flattenErrors walks a ValidationError tree and returns one entry per leaf
// error, with the JSON Pointer path to the failing field.
func flattenErrors(err error) []ValidationError {
	var verr *jsonschema.ValidationError
	ok := errors.As(err, &verr)
	if !ok {
		return []ValidationError{{Msg: err.Error()}}
	}
	basic := verr.BasicOutput()
	var out []ValidationError
	for _, unit := range basic.Errors {
		if unit.Error == nil {
			continue
		}
		out = append(out, ValidationError{
			Path: unit.InstanceLocation,
			Msg:  unit.Error.String(),
		})
	}
	if len(out) == 0 {
		out = append(out, ValidationError{Msg: err.Error()})
	}
	return out
}
