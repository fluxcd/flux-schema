// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
)

// SchemaLoader resolves, fetches, and compiles JSON Schemas from a set of
// location templates. Each rendered location is fetched at most once per
// loader lifetime; the compiled *jsonschema.Schema is cached for reuse.
//
// SchemaLoader is safe for concurrent use.
type SchemaLoader struct {
	templates   []*template.Template
	httpClient  *retryablehttp.Client
	httpTimeout time.Duration

	compiler *jsonschema.Compiler
	// compileMu guards compiler.AddResource + compiler.Compile.
	// jsonschema.Compiler is not safe for concurrent use; the per-entry
	// sync.Once only dedupes work per location, so cross-location calls
	// still need serialization.
	compileMu sync.Mutex
	cache     sync.Map // map[string]*schemaCacheEntry
}

// loadResult is the populated outcome of a single loadAndCompile call.
// celBuildErr is recorded as data, not an error return: it means "this
// schema's CEL evaluator could not be built" (e.g. apiextensions/v1 cannot
// decode the shape) and is surfaced per-document by validateDoc, never as a
// schema-load failure. The hard schema-load error stays on the cache entry.
type loadResult struct {
	schema      *jsonschema.Schema
	cel         *celValidator
	celBuildErr error
	found       bool
}

type schemaCacheEntry struct {
	once sync.Once
	loadResult
	err error
}

// ResolvedSchema is the bundle of artifacts the loader produces for one
// schema location: the compiled JSON Schema, plus an optional compiled CEL
// evaluator. Either of the CEL fields may be set independently — the schema
// may have rules that compiled fine (CEL set, CELBuildErr nil), or rules
// that failed to set up (CEL nil, CELBuildErr non-nil), or no rules at all
// (both nil).
type ResolvedSchema struct {
	JSON        *jsonschema.Schema
	CEL         *celValidator
	CELBuildErr error
	Location    string
}

// NewSchemaLoader returns a loader that will try templates in order and
// fetch each rendered location through httpClient (for http/https) or the
// local filesystem. httpTimeout, when > 0, wraps each HTTP request with a
// derived context.
func NewSchemaLoader(templates []*template.Template, httpClient *retryablehttp.Client, httpTimeout time.Duration) *SchemaLoader {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	registerKubernetesFormats(compiler)

	return &SchemaLoader{
		templates:   templates,
		httpClient:  httpClient,
		httpTimeout: httpTimeout,
		compiler:    compiler,
	}
}

// Resolve renders each template with vars and returns the first schema that
// exists. found=false means every location responded with 404 / ENOENT;
// callers decide whether that is an error or a skip.
func (l *SchemaLoader) Resolve(ctx context.Context, vars tmpl.SchemaVars) (resolved *ResolvedSchema, found bool, err error) {
	for _, tpl := range l.templates {
		rendered, rerr := tmpl.Execute(tpl, vars)
		if rerr != nil {
			return nil, false, rerr
		}
		entry := l.cacheEntry(rendered)
		entry.once.Do(func() {
			entry.loadResult, entry.err = l.loadAndCompile(ctx, rendered)
		})
		if entry.err != nil {
			return nil, false, fmt.Errorf("%s: %w", rendered, entry.err)
		}
		if entry.found {
			return &ResolvedSchema{
				JSON:        entry.schema,
				CEL:         entry.cel,
				CELBuildErr: entry.celBuildErr,
				Location:    rendered,
			}, true, nil
		}
	}
	return nil, false, nil
}

func (l *SchemaLoader) cacheEntry(location string) *schemaCacheEntry {
	if existing, ok := l.cache.Load(location); ok {
		return existing.(*schemaCacheEntry)
	}
	entry, _ := l.cache.LoadOrStore(location, &schemaCacheEntry{})
	return entry.(*schemaCacheEntry)
}

// loadAndCompile fetches location and compiles its contents as a JSON
// Schema, then builds a CEL evaluator from the same document. The returned
// loadResult has found=false when the location responded with "not found",
// so the caller can fall through to the next template.
//
// CEL build is intentionally performed AFTER compileMu is released:
// compileMu only guards compiler.AddResource/Compile, and holding it across
// CEL construction would serialize unrelated schemas' CEL builds. A CEL
// build failure is recorded on the result (celBuildErr) rather than
// returned, because it must not block JSON Schema validation.
func (l *SchemaLoader) loadAndCompile(ctx context.Context, location string) (loadResult, error) {
	var r loadResult
	body, found, err := l.loadBytes(ctx, location)
	if err != nil || !found {
		return r, err
	}

	baseURI, err := baseURIFor(location)
	if err != nil {
		return r, err
	}

	var doc any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return r, fmt.Errorf("parse schema: %w", err)
	}

	r.schema, err = l.compileJSONSchema(baseURI, doc)
	if err != nil {
		return loadResult{}, err
	}
	r.found = true

	if rootMap, ok := doc.(map[string]any); ok {
		r.cel, r.celBuildErr = newCELValidator(rootMap)
	}
	return r, nil
}

// compileJSONSchema serializes access to the shared jsonschema.Compiler.
// jsonschema.Compiler is not safe for concurrent use; the per-entry sync.Once
// only dedupes work per location, so cross-location calls still need this
// mutex. Kept narrow so it doesn't serialize unrelated work (e.g. CEL build).
func (l *SchemaLoader) compileJSONSchema(baseURI string, doc any) (*jsonschema.Schema, error) {
	l.compileMu.Lock()
	defer l.compileMu.Unlock()

	if err := l.compiler.AddResource(baseURI, doc); err != nil {
		if !errors.As(err, new(*jsonschema.ResourceExistsError)) {
			return nil, fmt.Errorf("add schema resource: %w", err)
		}
	}
	schema, err := l.compiler.Compile(baseURI)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return schema, nil
}

// loadBytes fetches a schema from an http(s) URL or the local filesystem.
// A 404 or ENOENT returns found=false with no error.
func (l *SchemaLoader) loadBytes(ctx context.Context, location string) ([]byte, bool, error) {
	if isHTTPURL(location) {
		return l.loadHTTP(ctx, location)
	}
	return loadFile(location)
}

func (l *SchemaLoader) loadHTTP(ctx context.Context, location string) ([]byte, bool, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, false, err
	}
	if l.httpTimeout > 0 {
		reqCtx, cancel := context.WithTimeout(ctx, l.httpTimeout)
		defer cancel()
		req = req.WithContext(reqCtx)
	}
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("GET %s: %s", location, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

func loadFile(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// baseURIFor returns the URI to use as the compiler base for this location.
// HTTP URLs pass through unchanged; file paths are converted to file://
// URIs with correct URL encoding.
func baseURIFor(location string) (string, error) {
	if isHTTPURL(location) {
		return location, nil
	}
	abs, err := filepath.Abs(location)
	if err != nil {
		return "", err
	}
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String(), nil
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
