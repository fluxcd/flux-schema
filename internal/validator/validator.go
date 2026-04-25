// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
//
// When Final is true the result is a synthetic source-complete sentinel
// emitted by ValidateSources after every document for Source has been
// processed. Sentinels carry only Source; consumers should ignore them
// for counting and printing and use them solely to advance per-source
// streaming state.
type Result struct {
	Source     string
	DocIndex   int
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Status     Status
	Reason     Reason
	Errors     []ValidationError
	Final      bool
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
//
// InsecureSkipTLSVerify is only applied when HTTPClient is nil and the
// validator constructs its own client. Callers who supply HTTPClient are
// responsible for configuring TLS on that client themselves.
//
// Stdin is read when an input path passed to ValidateSources equals
// StdinSource. It is the caller's responsibility to pass a non-nil reader
// (typically os.Stdin) when that sentinel is used.
type Options struct {
	SchemaLocations       []string
	SkipMissingSchemas    bool
	SkipKinds             []string
	SkipJSONPaths         []string
	HTTPClient            *retryablehttp.Client
	HTTPTimeout           time.Duration
	Workers               int
	InsecureSkipTLSVerify bool
	Stdin                 io.Reader
}

// Validator resolves and applies JSON Schemas to Kubernetes manifests.
// It is safe for concurrent use by multiple goroutines.
type Validator struct {
	opts      Options
	loader    *SchemaLoader
	skipKinds []skipKindMatcher
	skipPaths []skipPathMatcher
}

// skipKindMatcher matches a document by Kind, optionally scoped to an
// apiVersion. An empty apiVersion matches any group/version.
type skipKindMatcher struct {
	apiVersion string
	kind       string
}

// parseSkipKind parses a SkipKinds pattern. Accepted shapes:
//
//	Kind              e.g. "Secret"
//	apiVersion/Kind   e.g. "v1/Secret", "source.toolkit.fluxcd.io/v1/GitRepository"
func parseSkipKind(s string) (skipKindMatcher, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return skipKindMatcher{}, errors.New("skip kind pattern must not be empty")
	}
	parts := strings.Split(s, "/")
	if slices.Contains(parts, "") {
		return skipKindMatcher{}, fmt.Errorf("skip kind pattern %q: segments must not be empty", s)
	}
	kind := parts[len(parts)-1]
	var apiVersion string
	if len(parts) > 1 {
		apiVersion = strings.Join(parts[:len(parts)-1], "/")
	}
	return skipKindMatcher{apiVersion: apiVersion, kind: kind}, nil
}

func (m skipKindMatcher) matches(apiVersion, kind string) bool {
	if m.kind != kind {
		return false
	}
	return m.apiVersion == "" || m.apiVersion == apiVersion
}

// skipPathMatcher targets a JSON Pointer for deletion before schema.Validate
// runs, optionally scoped to a Kind / apiVersion-Kind via the same selector
// rules as skipKindMatcher. An empty kind matches any Kind.
type skipPathMatcher struct {
	apiVersion string
	kind       string
	segments   []string
}

// parseSkipJSONPath parses a SkipJSONPaths pattern. Accepted shapes:
//
//	/foo/bar                          strip on every doc
//	Kind:/foo/bar                     scope by Kind
//	apiVersion/Kind:/foo/bar          scope by apiVersion+Kind
//	group/version/Kind:/foo/bar       scope by full GVK
//
// A leading '/' marks the input as a pure pointer with no selector, so
// pointer keys may freely contain ':' (e.g. '/metadata/annotations/foo:bar').
// Otherwise the first ':' separates the selector half (parsed by parseSkipKind)
// from the pointer half, which follows RFC 6901; '~1' decodes to '/' and '~0'
// to '~'. Pointer descent through arrays is a silent no-op (map keys only).
func parseSkipJSONPath(s string) (skipPathMatcher, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return skipPathMatcher{}, errors.New("skip JSON path pattern must not be empty")
	}
	var selector, pointer string
	switch {
	case strings.HasPrefix(s, "/"):
		// Pure pointer; preserves any ':' inside keys.
		pointer = s
	default:
		left, right, hasSelector := strings.Cut(s, ":")
		if !hasSelector {
			return skipPathMatcher{}, fmt.Errorf("skip JSON path pattern %q: pointer must start with '/'", s)
		}
		selector, pointer = left, right
	}
	if !strings.HasPrefix(pointer, "/") {
		return skipPathMatcher{}, fmt.Errorf("skip JSON path pattern %q: pointer must start with '/'", s)
	}
	if pointer == "/" {
		return skipPathMatcher{}, fmt.Errorf("skip JSON path pattern %q: pointer must target a property", s)
	}
	var apiVersion, kind string
	if selector != "" {
		m, err := parseSkipKind(selector)
		if err != nil {
			return skipPathMatcher{}, fmt.Errorf("skip JSON path pattern %q: %w", s, err)
		}
		apiVersion, kind = m.apiVersion, m.kind
	}
	rawSegments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	segments := make([]string, len(rawSegments))
	for i, seg := range rawSegments {
		if seg == "" {
			return skipPathMatcher{}, fmt.Errorf("skip JSON path pattern %q: empty segment in pointer", s)
		}
		// RFC 6901: decode '~1' before '~0' so '~01' decodes to literal '~1'
		// rather than collapsing into '/'.
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		segments[i] = seg
	}
	return skipPathMatcher{apiVersion: apiVersion, kind: kind, segments: segments}, nil
}

func (m skipPathMatcher) matches(apiVersion, kind string) bool {
	if m.kind != "" && m.kind != kind {
		return false
	}
	return m.apiVersion == "" || m.apiVersion == apiVersion
}

// stripPath deletes the field referenced by m.segments from doc when present.
// Missing keys and non-object containers along the path are silent no-ops, so
// a single unscoped pattern (e.g. '/sops') can be left on across a mixed tree
// without falsely modifying unrelated kinds.
func (m skipPathMatcher) stripPath(doc map[string]any) {
	parent := doc
	for i, seg := range m.segments {
		if i == len(m.segments)-1 {
			delete(parent, seg)
			return
		}
		next, ok := parent[seg].(map[string]any)
		if !ok {
			return
		}
		parent = next
	}
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
		opts.Workers = DefaultWorkers
	}
	if opts.HTTPClient == nil {
		c := retryablehttp.NewClient()
		c.Logger = nil
		if opts.InsecureSkipTLSVerify {
			applyInsecureTLS(c)
		}
		opts.HTTPClient = c
	}

	skipKinds := make([]skipKindMatcher, 0, len(opts.SkipKinds))
	for _, s := range opts.SkipKinds {
		m, err := parseSkipKind(s)
		if err != nil {
			return nil, err
		}
		skipKinds = append(skipKinds, m)
	}

	skipPaths := make([]skipPathMatcher, 0, len(opts.SkipJSONPaths))
	for _, s := range opts.SkipJSONPaths {
		m, err := parseSkipJSONPath(s)
		if err != nil {
			return nil, err
		}
		skipPaths = append(skipPaths, m)
	}

	return &Validator{
		opts:      opts,
		loader:    NewSchemaLoader(templates, opts.HTTPClient, opts.HTTPTimeout),
		skipKinds: skipKinds,
		skipPaths: skipPaths,
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
	// sourceWG counts in-flight jobs for source. Workers Done it when the
	// job is processed; the per-source waiter goroutine waits on it then
	// emits a Final sentinel so the consumer can advance its streaming
	// pointer past this source.
	sourceWG *sync.WaitGroup
}

// ValidateSources streams validation results for every document found in
// paths. Files are walked sequentially by a single producer goroutine;
// documents are validated by a pool of opts.Workers goroutines.
//
// After every document for a source has been validated and its Result
// pushed to the channel, a synthetic Result with Final=true is emitted for
// that source. Consumers can use these sentinels to advance per-source
// streaming state (e.g. to start flushing source N+1 as soon as source N
// is fully drained, instead of buffering until end-of-stream). Sentinels
// always arrive after every real Result for the same source because the
// waiter only fires once the per-source WaitGroup hits zero, which only
// happens after every worker has both pushed its Result and called Done.
//
// The returned channel is closed once all documents have been processed
// and all sentinels have been delivered.
func (v *Validator) ValidateSources(ctx context.Context, paths []string) <-chan Result {
	results := make(chan Result, v.opts.Workers*2)
	jobs := make(chan job, v.opts.Workers*2)

	var workerWG sync.WaitGroup
	for i := 0; i < v.opts.Workers; i++ {
		workerWG.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					v.runJob(ctx, j, results)
				}
			}
		})
	}

	go func() {
		var waiterWG sync.WaitGroup
		spawnWaiter := func(src string, wg *sync.WaitGroup) {
			waiterWG.Go(func() {
				wg.Wait()
				select {
				case results <- Result{Source: src, Final: true}:
				case <-ctx.Done():
				}
			})
		}

		for _, path := range paths {
			if err := v.produceFromPath(ctx, path, jobs, spawnWaiter); err != nil {
				wg := &sync.WaitGroup{}
				wg.Add(1)
				select {
				case jobs <- job{source: path, loadErr: err, sourceWG: wg}:
					spawnWaiter(path, wg)
				case <-ctx.Done():
					wg.Done()
				}
			}
			if ctx.Err() != nil {
				break
			}
		}

		close(jobs)
		workerWG.Wait()
		// Drain any jobs that workers left behind when ctx was cancelled:
		// streamFile calls sourceWG.Add(1) before the send, and a worker's
		// select may pick <-ctx.Done() over <-jobs even when both are ready,
		// leaving queued jobs with unbalanced Add counts. Without this drain
		// per-source waiters would block forever and results would never close.
		for j := range jobs {
			j.sourceWG.Done()
		}
		waiterWG.Wait()
		close(results)
	}()

	return results
}

// runJob processes one job and always Done's its sourceWG so the per-source
// waiter can fire even when validation is short-circuited (content-free doc,
// ctx cancellation).
func (v *Validator) runJob(ctx context.Context, j job, results chan<- Result) {
	defer j.sourceWG.Done()
	var r Result
	emit := true
	if j.loadErr != nil {
		r = Result{
			Source:   j.source,
			DocIndex: j.docIndex,
			Status:   StatusInvalid,
			Reason:   ReasonSourceLoadError,
			Errors:   []ValidationError{{Msg: j.loadErr.Error()}},
		}
	} else {
		r, emit = v.validateDoc(ctx, j.source, j.docIndex, j.raw)
	}
	if !emit {
		return
	}
	select {
	case results <- r:
	case <-ctx.Done():
	}
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
// YAML document into jobs. spawnWaiter is invoked once per discovered file
// with a per-file WaitGroup the worker pool decrements; the validator then
// emits a Final sentinel once that file is fully drained.
//
// Returns a source-level error only for paths that cannot be stat'd or
// opened; per-document failures are reported via validateDoc inside the
// worker pool.
func (v *Validator) produceFromPath(ctx context.Context, path string, jobs chan<- job, spawnWaiter func(string, *sync.WaitGroup)) error {
	if path == StdinSource {
		if v.opts.Stdin == nil {
			return fmt.Errorf("source %q requires Options.Stdin to be set", StdinSource)
		}
		wg := &sync.WaitGroup{}
		err := v.streamReader(ctx, StdinSource, v.opts.Stdin, jobs, wg)
		spawnWaiter(StdinSource, wg)
		return err
	}
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
			wg := &sync.WaitGroup{}
			err := v.streamFile(ctx, p, jobs, wg)
			spawnWaiter(p, wg)
			return err
		})
	}
	wg := &sync.WaitGroup{}
	err = v.streamFile(ctx, path, jobs, wg)
	spawnWaiter(path, wg)
	return err
}

func (v *Validator) streamFile(ctx context.Context, path string, jobs chan<- job, sourceWG *sync.WaitGroup) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return v.streamReader(ctx, path, f, jobs, sourceWG)
}

// streamReader splits r into YAML documents and pushes them into the job
// channel under the given source label. Used by streamFile for on-disk
// inputs and by produceFromPath directly for the StdinSource sentinel so
// stdin doesn't go through a platform-specific path like "/dev/stdin".
func (v *Validator) streamReader(ctx context.Context, source string, r io.Reader, jobs chan<- job, sourceWG *sync.WaitGroup) error {
	scanner := yamldoc.NewScanner(r)
	idx := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if isContentFree(raw) {
			continue
		}
		idx++
		buf := make([]byte, len(raw))
		copy(buf, raw)
		sourceWG.Add(1)
		select {
		case <-ctx.Done():
			sourceWG.Done()
			return ctx.Err()
		case jobs <- job{source: source, docIndex: idx, raw: buf, sourceWG: sourceWG}:
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", source, err)
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
	settle := func(s Status, reason Reason) (Result, bool) {
		r.Status = s
		r.Reason = reason
		return r, true
	}
	// missingSchemaStatus picks between StatusSkipped and StatusInvalid for
	// the "schema file not found for a resolved GVK" case, honoring
	// Options.SkipMissingSchemas. The other no-schema case (missing
	// apiVersion/kind) branches on SkipMissingSchemas directly earlier in
	// this function, since it maps to different reason codes.
	missingSchemaStatus := func() Status {
		if v.opts.SkipMissingSchemas {
			return StatusSkipped
		}
		return StatusInvalid
	}

	var doc map[string]any
	if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
		// Lenient re-parse so callers can still render Kind/Namespace/Name
		// for a doc that failed admission (e.g. duplicate keys), rather
		// than the anonymous "/#1".
		var lenient map[string]any
		if yaml.Unmarshal(raw, &lenient) == nil && lenient != nil {
			r.APIVersion, r.Kind, r.Namespace, r.Name = extractIdentity(lenient)
		}
		if r.Name == "" {
			r.Name = fmt.Sprintf("#%d", idx)
		}
		r.Errors = splitYAMLError(err)
		return settle(StatusInvalid, ReasonYAMLParseError)
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

	// SkipKinds matching runs before admission and schema checks so a
	// kind-only entry (e.g. "Secret") also covers sealed/encrypted manifests
	// that would otherwise fail the name/generateName rule.
	for _, m := range v.skipKinds {
		if m.matches(r.APIVersion, r.Kind) {
			return settle(StatusSkipped, ReasonKindSkipped)
		}
	}

	if r.APIVersion == "" || r.Kind == "" {
		// With --skip-missing-schemas we can't do anything useful for a doc
		// with no GVK to look up; treat it as "schema not found" and let the
		// user's skip policy decide. Otherwise surface path-based errors so
		// consumers know exactly which top-level fields the doc is missing.
		if v.opts.SkipMissingSchemas {
			r.Errors = []ValidationError{{
				Msg: "no schema: document is missing apiVersion/kind",
			}}
			return settle(StatusSkipped, ReasonSchemaNotFound)
		}
		if r.APIVersion == "" {
			r.Errors = append(r.Errors, ValidationError{
				Path: "/apiVersion",
				Msg:  "missing required property",
			})
		}
		if r.Kind == "" {
			r.Errors = append(r.Errors, ValidationError{
				Path: "/kind",
				Msg:  "missing required property",
			})
		}
		return settle(StatusInvalid, ReasonSchemaViolation)
	}

	if !hasIdentity {
		r.Errors = []ValidationError{{
			Path: "/metadata",
			Msg:  "missing property 'name' or 'generateName'",
		}}
		return settle(StatusInvalid, ReasonSchemaViolation)
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
		return settle(StatusInvalid, ReasonSchemaLoadError)
	}
	if !found {
		r.Errors = []ValidationError{{
			Msg: fmt.Sprintf("no schema for kind %q in version %q", r.Kind, r.APIVersion),
		}}
		return settle(missingSchemaStatus(), ReasonSchemaNotFound)
	}

	for _, m := range v.skipPaths {
		if m.matches(r.APIVersion, r.Kind) {
			m.stripPath(doc)
		}
	}

	if err := schema.Validate(doc); err != nil {
		r.Errors = flattenErrors(err)
		return settle(StatusInvalid, ReasonSchemaViolation)
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

// applyInsecureTLS clones the retryablehttp client's underlying transport and
// enables InsecureSkipVerify on its TLS config. Cloning avoids mutating the
// shared cleanhttp default transport, which would leak the setting into
// unrelated HTTP clients in the same process.
func applyInsecureTLS(c *retryablehttp.Client) {
	t, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t = &http.Transport{}
	} else {
		t = t.Clone()
	}
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	}
	t.TLSClientConfig.InsecureSkipVerify = true
	c.HTTPClient.Transport = t
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
