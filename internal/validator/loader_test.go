// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"text/template"

	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/flux-schema/internal/tmpl"
)

var widgetVars = tmpl.SchemaVars{Group: "example.com", Kind: "Widget", Version: "v1"}

// newTestLoader parses locations and wires a SchemaLoader backed by a
// no-retry retryablehttp client so HTTP tests fail fast.
func newTestLoader(t *testing.T, locations ...string) *SchemaLoader {
	t.Helper()
	parsed := make([]*template.Template, len(locations))
	for i, loc := range locations {
		tpl, err := tmpl.Parse(loc)
		if err != nil {
			t.Fatalf("parse template %q: %v", loc, err)
		}
		parsed[i] = tpl
	}
	client := retryablehttp.NewClient()
	client.Logger = nil
	client.RetryMax = 0
	return NewSchemaLoader(parsed, client, 0)
}

func TestSchemaLoader_Resolve_LocalFile(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	l := newTestLoader(t, filepath.Join(dir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"))

	schema, location, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(schema).ToNot(BeNil())
	g.Expect(location).To(Equal(filepath.Join(dir, "widget-example-v1.json")))
}

func TestSchemaLoader_Resolve_FileNotFoundReturnsNotFound(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	l := newTestLoader(t, filepath.Join(dir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"))

	_, _, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeFalse())
}

func TestSchemaLoader_Resolve_NoLocationConfigured(t *testing.T) {
	g := NewWithT(t)
	l := newTestLoader(t) // zero locations

	_, _, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeFalse())
}

func TestSchemaLoader_Resolve_MultipleLocationsFallthrough(t *testing.T) {
	g := NewWithT(t)
	missingDir := t.TempDir()
	realDir := t.TempDir()
	writeWidgetSchema(t, realDir)

	l := newTestLoader(t,
		filepath.Join(missingDir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"),
		filepath.Join(realDir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"),
	)

	_, location, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(location).To(HavePrefix(realDir))
}

func TestSchemaLoader_Resolve_HTTP200(t *testing.T) {
	g := NewWithT(t)
	schemaBody := simpleSchemaJSON(g)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(schemaBody)
	}))
	t.Cleanup(srv.Close)

	l := newTestLoader(t, srv.URL+"/{{ .Kind }}_{{ .Version }}.json")
	schema, location, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(schema).ToNot(BeNil())
	// tmpl.Execute lowercases the Kind variable before rendering.
	g.Expect(location).To(Equal(srv.URL + "/widget_v1.json"))
}

func TestSchemaLoader_Resolve_HTTP404Fallthrough(t *testing.T) {
	g := NewWithT(t)
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	realDir := t.TempDir()
	writeWidgetSchema(t, realDir)

	l := newTestLoader(t,
		srv.URL+"/{{ .Kind }}_{{ .Version }}.json",
		filepath.Join(realDir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"),
	)

	_, _, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(requestCount.Load()).To(Equal(int32(1)))
}

func TestSchemaLoader_Resolve_HTTP500ReturnsError(t *testing.T) {
	g := NewWithT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	l := newTestLoader(t, srv.URL+"/{{ .Kind }}_{{ .Version }}.json")
	_, _, _, err := l.Resolve(context.Background(), widgetVars)
	// retryablehttp classifies 5xx as retryable; with RetryMax=0 it returns
	// "giving up after 1 attempt(s)". Either wording satisfies "it errored".
	g.Expect(err).To(HaveOccurred())
}

func TestSchemaLoader_Resolve_InvalidSchemaBodyReturnsCompileError(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	bad := []byte(`"not an object"`)
	g.Expect(os.WriteFile(filepath.Join(dir, "widget-example-v1.json"), bad, 0o644)).To(Succeed())
	l := newTestLoader(t, filepath.Join(dir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"))

	_, _, _, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).To(HaveOccurred())
}

func TestSchemaLoader_Resolve_WithInternalRef(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	schema := map[string]any{
		"type": "object",
		"definitions": map[string]any{
			"ObjectMeta": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
		"properties": map[string]any{
			"metadata": map[string]any{"$ref": "#/definitions/ObjectMeta"},
		},
	}
	b, err := json.Marshal(schema)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(os.WriteFile(filepath.Join(dir, "widget-example-v1.json"), b, 0o644)).To(Succeed())

	l := newTestLoader(t, filepath.Join(dir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json"))
	s, _, found, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(s.Validate(map[string]any{"metadata": map[string]any{"name": "r1"}})).To(Succeed())
}

func TestSchemaLoader_Resolve_CachesAcrossCalls(t *testing.T) {
	g := NewWithT(t)
	var requestCount atomic.Int32
	body := simpleSchemaJSON(g)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	l := newTestLoader(t, srv.URL+"/{{ .Kind }}_{{ .Version }}.json")
	for range 5 {
		_, _, found, err := l.Resolve(context.Background(), widgetVars)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
	}
	g.Expect(requestCount.Load()).To(Equal(int32(1)))
}

func TestSchemaLoader_Resolve_ConcurrentDedup(t *testing.T) {
	g := NewWithT(t)
	var requestCount atomic.Int32
	body := simpleSchemaJSON(g)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	l := newTestLoader(t, srv.URL+"/{{ .Kind }}_{{ .Version }}.json")

	// Fire many concurrent Resolve calls for the same location; sync.Once
	// must coalesce them into a single HTTP fetch and compile.
	const N = 32
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := range N {
		wg.Go(func() {
			_, _, _, errs[i] = l.Resolve(context.Background(), widgetVars)
		})
	}
	wg.Wait()
	for _, err := range errs {
		g.Expect(err).ToNot(HaveOccurred())
	}
	g.Expect(requestCount.Load()).To(Equal(int32(1)))
}

func TestSchemaLoader_Resolve_TemplateExecuteError(t *testing.T) {
	g := NewWithT(t)
	// SchemaLoader accepts pre-parsed templates, so a template that parses
	// but references an unknown var must surface the error from Resolve
	// rather than being suppressed.
	l := newTestLoader(t, "{{ .Unknown }}")
	_, _, _, err := l.Resolve(context.Background(), widgetVars)
	g.Expect(err).To(HaveOccurred())
}

func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"http://example.com":  true,
		"https://example.com": true,
		"/etc/schema.json":    false,
		"file:///etc/x.json":  false,
		"ftp://example.com":   false,
		"":                    false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(isHTTPURL(in)).To(Equal(want))
		})
	}
}

func TestBaseURIFor_HTTPPassthrough(t *testing.T) {
	g := NewWithT(t)
	got, err := baseURIFor("https://example.com/schema.json")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal("https://example.com/schema.json"))
}

func TestBaseURIFor_LocalPathBecomesFileURI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path layout assumed")
	}
	g := NewWithT(t)
	dir := t.TempDir()
	got, err := baseURIFor(filepath.Join(dir, "schema.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(HavePrefix("file://"))
	g.Expect(got).To(HaveSuffix("/schema.json"))
}

// simpleSchemaJSON returns a permissive schema body for HTTP fixtures.
func simpleSchemaJSON(g Gomega) []byte {
	body, err := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec":       map[string]any{"type": "object"},
		},
	})
	g.Expect(err).ToNot(HaveOccurred())
	return body
}
