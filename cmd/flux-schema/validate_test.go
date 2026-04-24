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

// extractWidgetSchema dogfoods the extract → validate round-trip so
// validate tests run against the real artifact users produce.
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

	g.Expect(out).To(ContainSubstring(reconcilersPath + " - HelmRelease/apps/webapp is valid"))
	g.Expect(out).To(ContainSubstring(sourcesPath + " - Bucket/default/minio-bucket is valid"))

	g.Expect(out).To(ContainSubstring(sourcesPath + " - Secret/default/minio-bucket-secret is skipped: schema not found"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - no schema for kind "Secret" in version "v1"$`))

	// 11 docs in valid-reconcilers.yaml + 8 in valid-sources.yaml = 19; the
	// Secret is skipped, so the remaining 18 validate cleanly.
	g.Expect(out).To(ContainSubstring("Summary: 19 resources found in 2 files - Valid: 18, Invalid: 0, Skipped: 1"))
}

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

	g.Expect(out).To(ContainSubstring(invalidPath + " - Bucket/default/minio-bucket is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - HelmRepository/default/example is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - GitRepository/default/podinfo is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/podinfo is invalid: schema validation failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - ArtifactGenerator/apps/podinfo-composite is invalid: schema not found"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - no schema for kind "ArtifactGenerator" in version "source\.extensions\.fluxcd\.io/v1alpha1"$`))
	g.Expect(out).To(ContainSubstring(invalidPath + " - HelmChart/default/podinfo is valid"))

	g.Expect(out).To(ContainSubstring("  - /spec: missing property 'bucketName'"))
	g.Expect(out).To(ContainSubstring("  - /spec/interval: got number, want string"))
	g.Expect(out).To(ContainSubstring("  - /spec: additional properties 'force' not allowed"))
	g.Expect(out).To(ContainSubstring("  - /spec/insecure: got string, want boolean"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - /spec/interval: '5m0s2d' does not match pattern`))

	g.Expect(out).To(ContainSubstring("Summary: 6 resources found in 1 file - Valid: 1, Invalid: 5, Skipped: 0"))
}

// TestValidateCmd_InvalidMetadataFixtures covers admission short-circuits
// (duplicate keys, missing metadata.name) and the lenient re-parse that
// recovers Kind/Namespace/Name when strict decode fails.
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

	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-labels is invalid: YAML parse failed"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-fields is invalid: YAML parse failed"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 8: key "app" already set in map$`))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 11: key "tag" already set in map$`))

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
	g.Expect(out).To(ContainSubstring("Bucket/default/minio-bucket is valid"))
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

// TestValidateCmd_FailFast runs 50 invalid docs with --concurrent 1 so the
// cut-short count is near-deterministic: at most a handful of invalids,
// never the full 50.
func TestValidateCmd_FailFast(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()

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
	g.Expect(out).To(MatchRegexp(`Invalid: [1-9],`))
}

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

func TestValidateCmd_StdinDash(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	replaceStdin(t, validWidget)

	out, err := executeCommand([]string{
		"validate", "-",
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("stdin - Widget/default/ok-widget is valid"))
	g.Expect(out).To(ContainSubstring("parsing stdin"))
}

func TestValidateCmd_StdinBare(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	replaceStdin(t, validWidget)

	out, err := executeCommand([]string{
		"validate",
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("stdin - Widget/default/ok-widget is valid"))
}

func TestValidateCmd_NoArgsNoPipe(t *testing.T) {
	g := NewWithT(t)
	forceStdinTTY(t)
	_, err := executeCommand([]string{"validate"})
	g.Expect(err).To(MatchError(ContainSubstring("no input")))
}

func TestValidateCmd_MultipleStdinSentinels(t *testing.T) {
	g := NewWithT(t)
	_, err := executeCommand([]string{"validate", "-", "-"})
	g.Expect(err).To(MatchError(ContainSubstring("stdin may only be referenced once")))
}

// TestValidateCmd_InsecureSkipTLSVerify points --schema-location at an
// httptest TLS server using a self-signed cert — default run must fail,
// run with the flag must succeed.
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

	_, err = executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", srv.URL + "/{{.Kind}}_{{.Version}}.json",
	})
	g.Expect(err).To(HaveOccurred())

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

func TestValidateCmd_Config_AppliesFlagValues(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  verbose: true
  skip-kind:
    - Widget
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Widget/default/ok-widget is skipped: kind skipped"))
	g.Expect(out).To(ContainSubstring("Valid: 0, Invalid: 0, Skipped: 1"))
}

func TestValidateCmd_Config_CLIOverridesBool(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  verbose: true
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
		"--verbose=false",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).ToNot(ContainSubstring("is valid"))
	g.Expect(out).To(ContainSubstring("Valid: 1"))
}

// CLI --skip-kind replaces (does not merge with) the config's skip-kind list.
func TestValidateCmd_Config_CLIOverridesSlice(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  skip-kind:
    - Widget
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
		"--skip-kind", "Secret",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1, Invalid: 0, Skipped: 0"))
}

// Covers both *int apply paths: absent key preserves the default, explicit
// value (here 0) is applied and trips the >=1 guard.
func TestValidateCmd_Config_Concurrent(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  verbose: true
`)
	_, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())

	cfgZero := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  concurrent: 0
`)
	_, err = executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfgZero,
	})
	g.Expect(err).To(MatchError(ContainSubstring("--concurrent must be >= 1")))
}

func TestValidateCmd_Config_NoValidateSection(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
`)
	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1"))
}

func TestValidateCmd_Config_VersionMissing(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `validate:
  verbose: true
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring(`unsupported version ""`)))
}

func TestValidateCmd_Config_VersionUnsupported(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "2"
validate:
  verbose: true
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring(`unsupported version "2"`)))
}

func TestValidateCmd_Config_StrictUnknownKey(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  skip_kind:
    - Secret
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("skip_kind"))
}

func TestValidateCmd_Config_StrictUnknownSection(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
extracft:
  verbose: true
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("extracft"))
}

func TestValidateCmd_Config_FileNotFound(t *testing.T) {
	g := NewWithT(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	_, err := executeCommand([]string{"validate", "--config", missing})
	g.Expect(err).To(MatchError(ContainSubstring("read " + missing)))
}

// FLUX_SCHEMA_CONFIG env var is consulted when --config is not passed.
func TestValidateCmd_Config_EnvVar(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  skip-kind:
    - Widget
`)
	t.Setenv("FLUX_SCHEMA_CONFIG", cfg)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Skipped: 1"))
}

// --config wins over FLUX_SCHEMA_CONFIG when both are set.
func TestValidateCmd_Config_FlagBeatsEnvVar(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	// Env var config has unsupported version — must not be read.
	envCfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "99"`)
	t.Setenv("FLUX_SCHEMA_CONFIG", envCfg)

	flagCfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml", `version: "1"
validate:
  skip-kind:
    - Widget
`)
	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", flagCfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Skipped: 1"))
}

func TestValidateCmd_Config_MalformedYAML(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".flux-schema.yaml",
		"version: \"1\"\nvalidate:\n\tverbose: true\n") // tabs are illegal indent
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring("parse " + cfg)))
}

func TestExpandSchemaLocations(t *testing.T) {
	g := NewWithT(t)

	expand := func(in []string) []string {
		out, err := expandSchemaLocations(in)
		g.Expect(err).ToNot(HaveOccurred())
		return out
	}

	g.Expect(expand([]string{"default"})).
		To(Equal([]string{validator.DefaultSchemaLocation}))

	g.Expect(expand([]string{"DEFAULT", "Default"})).
		To(Equal([]string{validator.DefaultSchemaLocation, validator.DefaultSchemaLocation}))

	g.Expect(expand([]string{"./local/{{.Kind}}.json"})).
		To(Equal([]string{"./local/{{.Kind}}.json"}))

	g.Expect(expand([]string{"./my-schemas"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))

	g.Expect(expand([]string{"./my-schemas/"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))
	g.Expect(expand([]string{"./my-schemas///"})).
		To(Equal([]string{"./my-schemas/" + validator.DefaultSchemaLayout}))

	// Go accepts forward slashes on Windows, so backslashes normalize cleanly.
	g.Expect(expand([]string{`.\my-schemas\`})).
		To(Equal([]string{`.\my-schemas/` + validator.DefaultSchemaLayout}))

	g.Expect(expand([]string{"https://example.com/catalog"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout}))

	g.Expect(expand([]string{"https://example.com/catalog?ref=main"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "?ref=main"}))

	g.Expect(expand([]string{"https://example.com/catalog#v1"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "#v1"}))

	g.Expect(expand([]string{"https://example.com/catalog/?ref=main"})).
		To(Equal([]string{"https://example.com/catalog/" + validator.DefaultSchemaLayout + "?ref=main"}))

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

	_, err = expandSchemaLocations([]string{"   "})
	g.Expect(err).To(MatchError(ContainSubstring("--schema-location must not be empty")))
}
