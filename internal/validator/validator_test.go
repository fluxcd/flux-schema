// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/gomega"
)

// writeWidgetSchema writes a minimal CRD-style JSON Schema for testing.
// Mirrors the shape produced by `flux-schema extract` (closed objects via
// additionalProperties: false, required fields, string format hooks).
func writeWidgetSchema(t *testing.T, dir string) {
	t.Helper()
	schema := map[string]any{
		"type":     "object",
		"required": []any{"apiVersion", "kind", "spec"},
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"name"},
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"interval": map[string]any{"type": "string", "format": "duration"},
				},
			},
		},
	}
	b, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	path := filepath.Join(dir, "widget-example-v1.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
}

func newLocalValidator(t *testing.T, dir string, skipMissing bool) *Validator {
	t.Helper()
	v, err := New(Options{
		SchemaLocations:    []string{filepath.Join(dir, "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json")},
		SkipMissingSchemas: skipMissing,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestValidateBytes_ValidWidget(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
  namespace: default
spec:
  name: my-widget
  interval: 30m
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusValid))
	g.Expect(results[0].Identifier()).To(Equal("Widget/default/w1"))
	g.Expect(results[0].Errors).To(BeEmpty())
}

func TestValidateBytes_InvalidFieldType(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: 42
  interval: 30m
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Message).To(Equal("schema validation failed"))
	g.Expect(results[0].Errors).ToNot(BeEmpty())
	// At least one error must reference /spec/name
	found := false
	for _, e := range results[0].Errors {
		if e.Path == "/spec/name" {
			found = true
		}
	}
	g.Expect(found).To(BeTrue(), "expected an error on /spec/name, got %+v", results[0].Errors)
}

func TestValidateBytes_MultipleViolations(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	// This document triggers three independent violations under spec:
	//  - name is an integer, not a string (type)
	//  - interval is not a valid duration (format)
	//  - unknownField is rejected by additionalProperties: false
	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: 42
  interval: not-a-duration
  unknownField: nope
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Message).To(Equal("schema validation failed"))
	// flattenErrors must surface one entry per leaf cause; we don't pin the
	// exact count (jsonschema/v6 can emit multiple causes per keyword) but
	// every expected path must appear at least once.
	g.Expect(len(results[0].Errors)).To(BeNumerically(">=", 3))

	paths := make(map[string]bool)
	messages := make([]string, 0, len(results[0].Errors))
	for _, e := range results[0].Errors {
		paths[e.Path] = true
		messages = append(messages, e.Msg)
	}
	g.Expect(paths).To(HaveKey("/spec/name"))
	g.Expect(paths).To(HaveKey("/spec/interval"))
	// additionalProperties: false reports on the parent object; the rejected
	// field name shows up in the error message rather than the path.
	g.Expect(paths).To(HaveKey("/spec"))
	g.Expect(messages).To(ContainElement(ContainSubstring("unknownField")))
}

func TestValidateBytes_NonStandardDurations(t *testing.T) {
	// End-to-end regression for the kube-openapi duration port: units Go's
	// time.ParseDuration rejects ("2w", "3d", "22 ns") must still flow through
	// the full Validator pipeline as StatusValid, because kube-apiserver accepts
	// them at CRD admission. Unit coverage for the validator lives in
	// formats_test.go; this test guards the wiring from compiler registration
	// through Validate.
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	for _, d := range []string{"2w", "3d", "22 ns", "1h30m"} {
		t.Run(d, func(t *testing.T) {
			g := NewWithT(t)
			doc := fmt.Appendf(nil, `apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
  interval: %s
`, d)
			results := v.ValidateBytes(context.Background(), "test.yaml", doc)
			g.Expect(results).To(HaveLen(1))
			g.Expect(results[0].Status).To(Equal(StatusValid),
				"duration %q should validate; got %s: %s", d, results[0].Status, results[0].Message)
		})
	}
}

func TestValidateBytes_InvalidDurationFormat(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
  interval: not-a-duration
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
}

func TestValidateBytes_AdditionalPropertiesRejected(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
  unknownField: nope
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
}

func TestValidateBytes_MissingSchema_Error(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Message).To(Equal("schema not found"))
	g.Expect(results[0].Errors).To(ConsistOf(
		ValidationError{Msg: `no schema for kind "Widget" in version "example.com/v1"`},
	))
}

func TestValidateBytes_MissingSchema_Skipped(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	v := newLocalValidator(t, dir, true)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusSkipped))
}

func TestValidateBytes_LeadingCommentsAndSeparator(t *testing.T) {
	// YAML files commonly open with a comment header followed by `---` before
	// the first real document. The parser sees that leading section as an
	// empty document (unmarshal → nil); it must be dropped entirely, not
	// surfaced as a skipped result.
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`# This file contains Widget manifests.
# See README for details.
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: my-widget
---
# trailing comment block with nothing else
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: w2
spec:
  name: my-widget
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(2))
	g.Expect(results[0].Status).To(Equal(StatusValid))
	g.Expect(results[0].Name).To(Equal("w1"))
	g.Expect(results[1].Status).To(Equal(StatusValid))
	g.Expect(results[1].Name).To(Equal("w2"))
}

func TestValidateBytes_MultiDocMixedResults(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, true)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
spec:
  name: valid
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: w2
spec:
  name: 42
---
apiVersion: other.example.com/v1
kind: OtherThing
metadata:
  name: t1
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(3))
	g.Expect(results[0].Status).To(Equal(StatusValid))
	g.Expect(results[1].Status).To(Equal(StatusInvalid))
	g.Expect(results[2].Status).To(Equal(StatusSkipped))
}

func TestValidateBytes_ClusterScopedIdentifier(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: cluster-widget
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusValid))
	g.Expect(results[0].Namespace).To(BeEmpty())
	g.Expect(results[0].Identifier()).To(Equal("Widget/cluster-widget"))
}

func TestValidateBytes_GenerateNameIdentity(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  namespace: flux-system
  generateName: foo-
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusValid))
	g.Expect(results[0].Identifier()).To(Equal("Widget/flux-system/foo-{{ generateName }}"))
}

func TestValidateBytes_NoNameOrGenerateName(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	// No schema on disk AND skipMissing=true — check runs *before* schema
	// lookup, so the invalid result must still surface.
	v := newLocalValidator(t, dir, true)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  namespace: default
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	// Two-level rendering: the admission-rule violation lives in Errors as
	// a JSON Pointer + message pair, matching how JSON Schema violations
	// surface through flattenErrors.
	g.Expect(results[0].Message).To(Equal("validation failed"))
	g.Expect(results[0].Errors).To(ConsistOf(
		ValidationError{Path: "/metadata", Msg: "missing property 'name' or 'generateName'"},
	))
	g.Expect(results[0].Name).To(Equal("#1"))
}

func TestValidateBytes_DuplicateKey(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: w1
  name: w2
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	// Two-level rendering: top-line is the short summary, the library's
	// per-line details land in Errors so the CLI can indent them.
	g.Expect(results[0].Message).To(Equal("YAML parse failed"))
	g.Expect(results[0].Errors).ToNot(BeEmpty())
	// The last-wins lenient parse must recover identity fields so the CLI
	// line reads "Widget/.../w2" rather than "/#1".
	g.Expect(results[0].Kind).To(Equal("Widget"))
	g.Expect(results[0].Name).To(Equal("w2"))
	// Each detail entry has no JSON Pointer path (YAML errors aren't field-
	// scoped) but carries the library's per-line diagnostic.
	msgs := make([]string, 0, len(results[0].Errors))
	for _, e := range results[0].Errors {
		g.Expect(e.Path).To(BeEmpty())
		msgs = append(msgs, e.Msg)
	}
	g.Expect(strings.Join(msgs, "\n")).To(Or(
		ContainSubstring("already set in map"),
		ContainSubstring("duplicate"),
	))
}

func TestValidateBytes_DuplicateKey_IdentityRecoveredWithNamespace(t *testing.T) {
	// Covers the realistic shape (namespace + duplicate label keys) from
	// testdata/validate/manifests/invalid-metadata.yaml: strict decode must
	// fail, but the CLI line still needs Kind/Namespace/Name.
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: dup
  namespace: flux-system
  labels:
    app: a
    app: b
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Identifier()).To(Equal("Widget/flux-system/dup"))
}

func TestValidateBytes_DuplicateKey_FallsBackToDocIndexWhenNameUnset(t *testing.T) {
	// If strict decode fails AND no name is present, we still want an
	// identifier — falls back to "#{docIndex}". Guards the belt-and-suspenders
	// path in populateIdentityFromRaw.
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  namespace: default
  labels:
    app: a
    app: b
spec:
  name: ok
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Identifier()).To(Equal("Widget/default/#1"))
}

func TestValidateBytes_MissingApiVersion(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	v := newLocalValidator(t, dir, false)

	doc := []byte(`metadata:
  name: w1
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Message).To(ContainSubstring("missing apiVersion/kind"))
}

func TestValidateSources_ConcurrencyCacheDedup(t *testing.T) {
	g := NewWithT(t)
	var requestCount atomic.Int32
	// Build a valid schema to return on every request.
	schemaBody, err := json.Marshal(map[string]any{
		"type":     "object",
		"required": []any{"apiVersion", "kind"},
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec":       map[string]any{"type": "object"},
		},
	})
	g.Expect(err).ToNot(HaveOccurred())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(schemaBody)
	}))
	t.Cleanup(srv.Close)

	client := retryablehttp.NewClient()
	client.Logger = nil
	client.RetryMax = 0

	v, err := New(Options{
		SchemaLocations: []string{srv.URL + "/{{ .Kind }}_{{ .Version }}.json"},
		HTTPClient:      client,
	})
	g.Expect(err).ToNot(HaveOccurred())

	// Compose a multi-doc payload with the same GVK repeated many times.
	var payload []byte
	for i := range 50 {
		payload = fmt.Appendf(payload, `apiVersion: example.com/v1
kind: Widget
metadata:
  name: w%d
spec: {}
---
`, i)
	}

	// Write payload to a temp file and call ValidateSources.
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.yaml")
	g.Expect(os.WriteFile(path, payload, 0o644)).To(Succeed())

	ch := v.ValidateSources(context.Background(), []string{path})
	var count int
	for r := range ch {
		if r.Final {
			continue
		}
		g.Expect(r.Status).To(Equal(StatusValid), "unexpected status for doc %d: %s %s", r.DocIndex, r.Status, r.Message)
		count++
	}
	g.Expect(count).To(Equal(50))
	// Exactly one HTTP fetch thanks to the sync.Map + sync.Once cache.
	g.Expect(requestCount.Load()).To(Equal(int32(1)))
}

func TestValidateSources_WalksDirectory(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)

	manifestsDir := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(manifestsDir, "a.yaml"), []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: a
spec:
  name: ok
`), 0o644)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(manifestsDir, "b.yml"), []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: b
spec:
  name: ok
`), 0o644)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(manifestsDir, "ignored.txt"), []byte("not yaml"), 0o644)).To(Succeed())

	v := newLocalValidator(t, dir, false)
	ch := v.ValidateSources(context.Background(), []string{manifestsDir})
	var count int
	finals := map[string]bool{}
	for r := range ch {
		if r.Final {
			finals[r.Source] = true
			continue
		}
		g.Expect(r.Status).To(Equal(StatusValid))
		count++
	}
	g.Expect(count).To(Equal(2))
	// One Final sentinel per discovered file.
	g.Expect(finals).To(HaveLen(2))
}

// TestValidateSources_FinalSentinelOrdering pins the streaming protocol:
// every real Result for a source arrives strictly before that source's
// Final sentinel. Consumers rely on this to advance per-source streaming
// state without buffering the entire stream.
func TestValidateSources_FinalSentinelOrdering(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)

	manifestsDir := t.TempDir()
	const docsPerFile = 20
	for _, name := range []string{"a.yaml", "b.yaml", "c.yaml"} {
		var buf bytes.Buffer
		for i := range docsPerFile {
			if i > 0 {
				buf.WriteString("---\n")
			}
			fmt.Fprintf(&buf, "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: %s-%d\nspec:\n  name: ok\n", name, i)
		}
		g.Expect(os.WriteFile(filepath.Join(manifestsDir, name), buf.Bytes(), 0o644)).To(Succeed())
	}

	v := newLocalValidator(t, dir, false)
	ch := v.ValidateSources(context.Background(), []string{manifestsDir})

	seen := map[string]int{}
	for r := range ch {
		if r.Final {
			// At sentinel arrival every real Result for r.Source must already
			// have been delivered.
			g.Expect(seen[r.Source]).To(Equal(docsPerFile),
				"final sentinel for %s arrived before all docs", r.Source)
			seen[r.Source] = -1 // mark as finalized
			continue
		}
		g.Expect(seen[r.Source]).To(BeNumerically(">=", 0),
			"real result for %s arrived after its final sentinel", r.Source)
		seen[r.Source]++
	}

	g.Expect(seen).To(HaveLen(3))
	for src, count := range seen {
		g.Expect(count).To(Equal(-1), "no final sentinel for %s", src)
	}
}

func TestSplitYAMLError(t *testing.T) {
	cases := map[string]struct {
		in   string
		msgs []string
	}{
		"multi-line unmarshal errors": {
			in: "error converting YAML to JSON: yaml: unmarshal errors:\n  line 8: key \"app\" already set in map\n  line 11: key \"tag\" already set in map",
			msgs: []string{
				`line 8: key "app" already set in map`,
				`line 11: key "tag" already set in map`,
			},
		},
		"single-line converting prefix": {
			in:   "error converting YAML to JSON: yaml: line 5: did not find expected key",
			msgs: []string{"line 5: did not find expected key"},
		},
		"bare yaml prefix": {
			in:   "yaml: line 2: mapping values are not allowed in this context",
			msgs: []string{"line 2: mapping values are not allowed in this context"},
		},
		"no known prefix passes through": {
			in:   "something completely unexpected",
			msgs: []string{"something completely unexpected"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			errs := splitYAMLError(errors.New(tc.in))
			got := make([]string, 0, len(errs))
			for _, e := range errs {
				g.Expect(e.Path).To(BeEmpty())
				got = append(got, e.Msg)
			}
			g.Expect(got).To(Equal(tc.msgs))
		})
	}
}

func TestIsContentFree(t *testing.T) {
	cases := map[string]bool{
		"":                                    true,
		"   \n\t\n":                           true,
		"# a comment":                         true,
		"# line 1\n# line 2\n":                true,
		"  # indented comment\n\n# another\n": true,
		"apiVersion: v1":                      false,
		"# comment\napiVersion: v1":           false,
		"---":                                 false, // leading dash isn't a comment marker
		`value: "# not a comment"`:            false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(isContentFree([]byte(in))).To(Equal(want))
		})
	}
}

func TestNew_RejectsBadTemplate(t *testing.T) {
	g := NewWithT(t)
	_, err := New(Options{SchemaLocations: []string{"{{ .Unknown }}"}})
	g.Expect(err).To(HaveOccurred())
}

func TestNew_RequiresAtLeastOneLocation(t *testing.T) {
	g := NewWithT(t)
	_, err := New(Options{})
	g.Expect(err).To(HaveOccurred())
}
