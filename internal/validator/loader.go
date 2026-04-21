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

type schemaCacheEntry struct {
	once   sync.Once
	schema *jsonschema.Schema
	found  bool
	err    error
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
func (l *SchemaLoader) Resolve(ctx context.Context, vars tmpl.SchemaVars) (schema *jsonschema.Schema, location string, found bool, err error) {
	for _, tpl := range l.templates {
		rendered, rerr := tmpl.Execute(tpl, vars)
		if rerr != nil {
			return nil, "", false, rerr
		}
		entry := l.cacheEntry(rendered)
		entry.once.Do(func() {
			entry.schema, entry.found, entry.err = l.loadAndCompile(ctx, rendered)
		})
		if entry.err != nil {
			return nil, rendered, false, fmt.Errorf("%s: %w", rendered, entry.err)
		}
		if entry.found {
			return entry.schema, rendered, true, nil
		}
	}
	return nil, "", false, nil
}

func (l *SchemaLoader) cacheEntry(location string) *schemaCacheEntry {
	if existing, ok := l.cache.Load(location); ok {
		return existing.(*schemaCacheEntry)
	}
	entry, _ := l.cache.LoadOrStore(location, &schemaCacheEntry{})
	return entry.(*schemaCacheEntry)
}

// loadAndCompile fetches location and compiles its contents as a JSON
// Schema. Returns (nil, false, nil) when the location responded with
// "not found" so the caller can fall through to the next template.
func (l *SchemaLoader) loadAndCompile(ctx context.Context, location string) (*jsonschema.Schema, bool, error) {
	body, found, err := l.loadBytes(ctx, location)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	baseURI, err := baseURIFor(location)
	if err != nil {
		return nil, false, err
	}

	var doc any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, false, fmt.Errorf("parse schema: %w", err)
	}

	l.compileMu.Lock()
	defer l.compileMu.Unlock()

	if err := l.compiler.AddResource(baseURI, doc); err != nil {
		if !errors.As(err, new(*jsonschema.ResourceExistsError)) {
			return nil, false, fmt.Errorf("add schema resource: %w", err)
		}
	}
	schema, err := l.compiler.Compile(baseURI)
	if err != nil {
		return nil, false, fmt.Errorf("compile schema: %w", err)
	}
	return schema, true, nil
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
