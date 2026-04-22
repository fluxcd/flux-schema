// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
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

func TestExpandSchemaLocations(t *testing.T) {
	g := NewWithT(t)

	expand := func(in []string) []string {
		out, err := expandSchemaLocations(in)
		g.Expect(err).ToNot(HaveOccurred())
		return out
	}

	g.Expect(expand([]string{"default"})).
		To(Equal([]string{defaultValidateSchemaLocation}))

	g.Expect(expand([]string{"default", "./local/{{.Kind}}.json"})).
		To(Equal([]string{defaultValidateSchemaLocation, "./local/{{.Kind}}.json"}))

	g.Expect(expand([]string{"./local/{{.Kind}}.json", "default"})).
		To(Equal([]string{"./local/{{.Kind}}.json", defaultValidateSchemaLocation}))

	g.Expect(expand([]string{"./local/{{.Kind}}.json"})).
		To(Equal([]string{"./local/{{.Kind}}.json"}))

	g.Expect(expand([]string{"DEFAULT", "Default"})).
		To(Equal([]string{defaultValidateSchemaLocation, defaultValidateSchemaLocation}))

	_, err := expandSchemaLocations([]string{""})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))

	_, err = expandSchemaLocations([]string{"default", ""})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))
}
