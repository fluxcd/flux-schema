// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/flux-schema/internal/validator"
)

// extractWidgetSchema runs the extract crd command on minimalCRDYAML and returns
// the schema directory. Dogfoods extract → validate round-trip so tests
// exercise the real artifact users will point validate at. Pins the flat
// kubeval-style layout so validate tests keep exercising a single
// --schema-location template regardless of the extract default.
func extractWidgetSchema(t *testing.T) string {
	t.Helper()
	g := NewWithT(t)

	crdDir := t.TempDir()
	crdPath := filepath.Join(crdDir, "widget-crd.yaml")
	g.Expect(os.WriteFile(crdPath, []byte(minimalCRDYAML), 0o644)).To(Succeed())

	schemaDir := t.TempDir()
	_, err := executeCommand([]string{
		"extract", "crd", crdPath,
		"--output-dir", schemaDir,
		"--output-format", "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json",
	})
	g.Expect(err).ToNot(HaveOccurred())
	return schemaDir
}

func writeManifest(t *testing.T, dir, name, body string) string {
	t.Helper()
	g := NewWithT(t)
	path := filepath.Join(dir, name)
	g.Expect(os.WriteFile(path, []byte(body), 0o644)).To(Succeed())
	return path
}

const validWidget = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: ok-widget
  namespace: default
spec:
  name: hello
`

const invalidWidget = `apiVersion: example.com/v1
kind: Widget
metadata:
  name: bad-widget
  namespace: default
spec:
  name: 42
`

func TestValidateCmd_ValidManifest_QuietNoOutput(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	// Quiet mode: no per-doc lines, only the summary.
	g.Expect(out).ToNot(ContainSubstring("is valid"))
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

func TestValidateCmd_InvalidManifest_PrintsViolationAndFails(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	path := writeManifest(t, manifestDir, "bad.yaml", invalidWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring(path + " - Widget/default/bad-widget is invalid: schema validation failed"))
	// Two-level rendering: one indented line per violation.
	g.Expect(out).To(MatchRegexp(`(?m)^  - /spec/name: `))
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 0, Invalid: 1, Skipped: 0"))
}

func TestValidateCmd_MissingSchema_ErrorsWithoutIgnoreFlag(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)
	// Empty schema dir — no schema will be found.
	schemaDir := t.TempDir()

	_, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).To(HaveOccurred())
}

func TestValidateCmd_MissingSchema_SkippedWithIgnoreFlag(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)
	schemaDir := t.TempDir()

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-missing-schemas",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 0, Invalid: 0, Skipped: 1"))
}

func TestValidateCmd_Verbose_PrintsValidLines(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	path := writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir, "-v",
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring(path + " - Widget/default/ok-widget is valid"))
}

func TestValidateCmd_MissingNameOrGenerateName(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "noname.yaml", `apiVersion: example.com/v1
kind: Widget
metadata:
  namespace: default
spec:
  name: x
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-missing-schemas",
	})
	// Admission-rule check runs before schema resolution, so the result is
	// invalid even under --skip-missing-schemas.
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Widget/default/#1 is invalid: validation failed"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - /metadata: missing property 'name' or 'generateName'$`))
}

func TestValidateCmd_MultiDoc_CountsAndPlurals(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "multi.yaml", validWidget+"---\n"+invalidWidget+"---\n"+validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 3 resources found in 1 file - Valid: 2, Invalid: 1, Skipped: 0"))
}

// TestValidateCmd_FluxManifestsFixtures runs the CLI against the real Flux
// manifest fixtures in testdata/validate/ using the datreeio-style schema
// layout (Group/Kind_Version.json). Exercises the happy path end-to-end with
// real schemas across multiple files, including the --skip-missing-schemas
// behavior for the core Secret.
func TestValidateCmd_FluxManifestsFixtures(t *testing.T) {
	g := NewWithT(t)

	reconcilersPath := "./testdata/validate/manifests/valid-reconcilers.yaml"
	sourcesPath := "./testdata/validate/manifests/valid-sources.yaml"

	out, err := executeCommand([]string{
		"validate",
		reconcilersPath,
		sourcesPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--skip-missing-schemas",
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())

	// Spot-check one line from each file to prove both sources were walked
	// and that identifiers render as Kind/Namespace/Name.
	g.Expect(out).To(ContainSubstring(reconcilersPath + " - HelmRelease/apps/webapp is valid"))
	g.Expect(out).To(ContainSubstring(sourcesPath + " - Bucket/default/minio-bucket is valid"))

	// The Secret has no schema in testdata; --skip-missing-schemas turns
	// that into a skipped line.
	g.Expect(out).To(ContainSubstring(sourcesPath + " - Secret/default/minio-bucket-secret is skipped: schema not found"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - no schema for kind "Secret" in version "v1"$`))

	// 11 docs in valid-reconcilers.yaml + 8 in valid-sources.yaml = 19; 1 Secret
	// is skipped, the remaining 18 validate cleanly.
	g.Expect(out).To(ContainSubstring("Summary: 19 resources found in 2 files - Valid: 18, Invalid: 0, Skipped: 1"))
}

// TestValidateCmd_InvalidFluxManifestsFixtures exercises the full failure
// surface against a hand-crafted invalid Flux manifest: schema violations
// (missing required, wrong type, additionalProperties, pattern mismatch) and
// a GVK with no registered schema. Also pins the summary counts and
// non-zero exit.
func TestValidateCmd_InvalidFluxManifestsFixtures(t *testing.T) {
	g := NewWithT(t)

	invalidPath := "./testdata/validate/manifests/invalid-sources.yaml"

	out, err := executeCommand([]string{
		"validate",
		invalidPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--verbose",
	})
	g.Expect(err).To(HaveOccurred())

	// Top-line rendering per failure mode.
	g.Expect(out).To(ContainSubstring(invalidPath + " - Bucket/default/minio-bucket is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - HelmRepository/default/example is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - GitRepository/default/podinfo is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/podinfo is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - ArtifactGenerator/apps/podinfo-composite is invalid: schema not found"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - no schema for kind "ArtifactGenerator" in version "source\.extensions\.fluxcd\.io/v1alpha1"$`))
	// A valid doc in the same file is still reported valid under --verbose.
	g.Expect(out).To(ContainSubstring(invalidPath + " - HelmChart/default/podinfo is valid"))

	// Representative field-level violations, covering each jsonschema/v6 error
	// class surfaced by this fixture: missing required, wrong type,
	// additionalProperties, pattern mismatch.
	g.Expect(out).To(ContainSubstring("  - /spec: missing property 'bucketName'"))
	g.Expect(out).To(ContainSubstring("  - /spec/interval: got number, want string"))
	g.Expect(out).To(ContainSubstring("  - /spec: additional properties 'force' not allowed"))
	g.Expect(out).To(ContainSubstring("  - /spec/insecure: got string, want boolean"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - /spec/interval: '5m0s2d' does not match pattern`))

	g.Expect(out).To(ContainSubstring("Summary: 6 resources found in 1 file - Valid: 1, Invalid: 5, Skipped: 0"))
}

// TestValidateCmd_InvalidMetadataFixtures covers three Kubernetes-admission
// failure modes that short-circuit before JSON Schema validation: strict-YAML
// duplicate keys (both in metadata.labels and in nested spec.ref), and a
// document missing metadata.name. Also pins the identity-recovery path: even
// when strict decode fails, the CLI line must show Kind/Namespace/Name when
// those fields are present in the document.
func TestValidateCmd_InvalidMetadataFixtures(t *testing.T) {
	g := NewWithT(t)

	invalidPath := "./testdata/validate/manifests/invalid-metadata.yaml"

	out, err := executeCommand([]string{
		"validate",
		invalidPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--verbose",
	})
	g.Expect(err).To(HaveOccurred())

	// Duplicate-key failures render with the short summary on the top line
	// and each library detail on an indented continuation line, matching the
	// two-level layout used for JSON Schema violations. Identity fields are
	// recovered via the lenient re-parse so the line reads as the real
	// resource, not "/#N".
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-labels is invalid: YAML parse failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-fields is invalid: YAML parse failed"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 8: key "app" already set in map$`))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 11: key "tag" already set in map$`))

	// Missing-name admission rule: Kind/Namespace still render; Name falls
	// back to #{docIndex} because no name is declared.
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/#3 is invalid: validation failed"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - /metadata: missing property 'name' or 'generateName'$`))

	g.Expect(out).To(ContainSubstring("Summary: 3 resources found in 1 file - Valid: 0, Invalid: 3, Skipped: 0"))
}

// TestValidateCmd_SchemaLocationShorthand pins that a --schema-location value
// without a trailing ".json" template is auto-expanded to the catalog layout
// ("{Group}/{Kind}_{Version}.json"), so users can pass a bare directory that
// matches the default output of `flux-schema extract`.
func TestValidateCmd_SchemaLocationShorthand(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"validate",
		"./testdata/validate/manifests/valid-sources.yaml",
		"--schema-location", "./testdata/validate/schemas",
		"--skip-missing-schemas",
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())
	// A representative valid line proves the shorthand resolved to real schemas.
	g.Expect(out).To(ContainSubstring("Bucket/default/minio-bucket is valid"))
	// The Secret has no schema under that layout, so --skip-missing-schemas
	// turns it into a skipped line — confirms the layout is being rendered.
	g.Expect(out).To(ContainSubstring("Secret/default/minio-bucket-secret is skipped"))
}

// TestValidateCmd_SkipKind exercises all three accepted pattern shapes against
// the real Flux fixtures: a bare Kind, an apiVersion/Kind, and a
// group/version/Kind. Each doc that matches is skipped instead of being
// validated, with a short-circuit line reading "... is skipped: kind skipped".
func TestValidateCmd_SkipKind(t *testing.T) {
	g := NewWithT(t)

	reconcilersPath := "./testdata/validate/manifests/valid-reconcilers.yaml"
	sourcesPath := "./testdata/validate/manifests/valid-sources.yaml"

	out, err := executeCommand([]string{
		"validate",
		reconcilersPath,
		sourcesPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--skip-kind", "Secret",
		"--skip-kind", "source.toolkit.fluxcd.io/v1/GitRepository",
		"--skip-kind", "helm.toolkit.fluxcd.io/v2/HelmRelease",
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(out).To(ContainSubstring(sourcesPath + " - Secret/default/minio-bucket-secret is skipped: kind skipped"))
	g.Expect(out).To(ContainSubstring(" - GitRepository/"))
	g.Expect(out).To(ContainSubstring("is skipped: kind skipped"))
	g.Expect(out).To(ContainSubstring(" - HelmRelease/"))
}

// TestValidateCmd_FailFast pins the short-circuit behavior: a multi-doc file
// containing many invalid documents must still trigger a non-zero exit, but
// --fail-fast should stop counting well before every document runs. With
// --concurrent 1 the pool is near-deterministic — we expect exactly 1 or 2
// invalid lines (one picked up before the cancel fires, plus at most one
// already in the worker pipeline) and a matching summary count.
func TestValidateCmd_FailFast(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()

	// 50 invalid docs — the pool with a single worker will cancel long
	// before all of them are counted.
	var body []byte
	for i := range 50 {
		if i > 0 {
			body = append(body, []byte("---\n")...)
		}
		body = append(body, []byte(invalidWidget)...)
	}
	writeManifest(t, manifestDir, "multi.yaml", string(body))

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--fail-fast",
		"--concurrent", "1",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring("is invalid: schema validation failed"))
	// Single-digit invalid count proves fail-fast cut the run short of 50.
	g.Expect(out).To(MatchRegexp(`Invalid: [1-9],`))
}

// TestValidateCmd_ConcurrentFlag pins that --concurrent accepts a value and
// that an invalid (<1) value is rejected with a clear error.
func TestValidateCmd_ConcurrentFlag(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--concurrent", "2",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1"))

	_, err = executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--concurrent", "0",
	})
	g.Expect(err).To(MatchError(ContainSubstring("--concurrent must be >= 1")))
}

// TestValidateCmd_InsecureSkipTLSVerify points --schema-location at a TLS
// httptest server backed by a self-signed cert. The default run must fail
// with a TLS verification error; passing the flag must make it succeed.
func TestValidateCmd_InsecureSkipTLSVerify(t *testing.T) {
	g := NewWithT(t)

	schemaBody, err := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata":   map[string]any{"type": "object"},
			"spec":       map[string]any{"type": "object"},
		},
	})
	g.Expect(err).ToNot(HaveOccurred())

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(schemaBody)
	}))
	t.Cleanup(srv.Close)

	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	// Without --insecure-skip-tls-verify the self-signed cert must be rejected.
	_, err = executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", srv.URL + "/{{.Kind}}_{{.Version}}.json",
	})
	g.Expect(err).To(HaveOccurred())

	// With the flag, the same run succeeds.
	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", srv.URL + "/{{.Kind}}_{{.Version}}.json",
		"--insecure-skip-tls-verify",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1"))
}

func TestValidateCmd_SkipKind_Invalid(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	_, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-kind", "v1/",
	})
	g.Expect(err).To(MatchError(ContainSubstring("skip kind pattern")))
}

func TestExpandSchemaLocations(t *testing.T) {
	g := NewWithT(t)

	expand := func(in []string) []string {
		out, err := expandSchemaLocations(in)
		g.Expect(err).ToNot(HaveOccurred())
		return out
	}

	// "default" alias expands to the hosted catalog URL.
	g.Expect(expand([]string{"default"})).
		To(Equal([]string{validator.DefaultSchemaLocation}))

	g.Expect(expand([]string{"DEFAULT", "Default"})).
		To(Equal([]string{validator.DefaultSchemaLocation, validator.DefaultSchemaLocation}))

	// Values ending in .json are taken verbatim as full templates.
	g.Expect(expand([]string{"./local/{{.Kind}}.json"})).
		To(Equal([]string{"./local/{{.Kind}}.json"}))

	// Bare paths get the catalog layout appended.
	g.Expect(expand([]string{"./my-schemas"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))

	// Trailing slashes are collapsed so the result has exactly one separator.
	g.Expect(expand([]string{"./my-schemas/"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))
	g.Expect(expand([]string{"./my-schemas///"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))

	// Backslashes are also trimmed so Windows-style paths ("\", "/") produce
	// a clean single-separator join. Go accepts forward slashes on Windows.
	g.Expect(expand([]string{`.\my-schemas\`})).
		To(Equal([]string{`.\my-schemas/` + validator.DefaultSchemaLayout}))

	// Bare URLs get the same tail appended; protocol is untouched.
	g.Expect(expand([]string{"https://example.com/catalog"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout}))

	// URL query string is preserved on the right of the template tail so the
	// template lands on the path, not inside the query.
	g.Expect(expand([]string{"https://example.com/catalog?ref=main"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "?ref=main"}))

	// URL fragment is handled the same way as a query string.
	g.Expect(expand([]string{"https://example.com/catalog#v1"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "#v1"}))

	// Trailing slash before the query is also collapsed.
	g.Expect(expand([]string{"https://example.com/catalog/?ref=main"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "?ref=main"}))

	// Mixed inputs preserve order and apply each rule independently.
	g.Expect(expand([]string{"default", "./local/{{.Kind}}.json"})).
		To(Equal([]string{validator.DefaultSchemaLocation, "./local/{{.Kind}}.json"}))

	g.Expect(expand([]string{"./local/{{.Kind}}.json", "default"})).
		To(Equal([]string{"./local/{{.Kind}}.json", validator.DefaultSchemaLocation}))

	g.Expect(expand([]string{"default", "./my-schemas"})).
		To(Equal([]string{validator.DefaultSchemaLocation, "./my-schemas/" + validator.DefaultSchemaLayout}))

	_, err := expandSchemaLocations([]string{""})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))

	_, err = expandSchemaLocations([]string{"default", ""})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))

	// Whitespace-only values are rejected the same way as empty strings.
	_, err = expandSchemaLocations([]string{"   "})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))
}
