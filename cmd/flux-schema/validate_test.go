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
	"github.com/santhosh-tekuri/jsonschema/v6"
	k8syaml "sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/validator"
)

// extractCRDSchema dogfoods the extract → validate round-trip so validate
// tests run against the real artifact users produce. crdYAML is a single CRD
// document; the returned dir holds the generated schema files.
func extractCRDSchema(t *testing.T, crdYAML string) string {
	t.Helper()
	g := NewWithT(t)

	crdDir := t.TempDir()
	crdPath := filepath.Join(crdDir, "crd.yaml")
	g.Expect(os.WriteFile(crdPath, []byte(crdYAML), 0o644)).To(Succeed())

	schemaDir := t.TempDir()
	_, err := executeCommand([]string{
		"extract", "crd", crdPath,
		"--output-dir", schemaDir,
		"--output-format", "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json",
	})
	g.Expect(err).ToNot(HaveOccurred())
	return schemaDir
}

func extractWidgetSchema(t *testing.T) string {
	t.Helper()
	return extractCRDSchema(t, minimalCRDYAML)
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
	g.Expect(out).To(ContainSubstring(path + " - Widget/default/bad-widget is invalid: schema violation"))
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

func TestValidateCmd_SchemaLocationFlagShorthand(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	path := writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir, "-v",
		"-s", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
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
	g.Expect(out).To(ContainSubstring("Widget/default/#1 is invalid: schema violation"))
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
// manifest fixtures in testdata/validate/ using the default catalog schema
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

	g.Expect(out).To(ContainSubstring(sourcesPath + " - SealedSecret/default/minio-bucket-secret is skipped: schema not found"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - no schema for kind "SealedSecret" in version "bitnami.com/v1alpha1"$`))

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

	g.Expect(out).To(ContainSubstring(invalidPath + " - Bucket/default/minio-bucket is invalid: schema violation"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - HelmRepository/default/example is invalid: schema violation"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - GitRepository/default/podinfo is invalid: schema violation"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/podinfo is invalid: schema violation"))
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
// (duplicate keys, missing metadata.name), the lenient re-parse that
// recovers Kind/Namespace/Name when strict decode fails, and the
// ObjectMeta checks that run alongside schema validation (DNS-1123
// namespace, label values, annotation keys with JSON Pointer escaping).
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

	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/apps.default/invalid-labels-and-annotations is invalid: schema violation"))
	g.Expect(out).To(ContainSubstring("  - /metadata/namespace: must not contain dots"))
	g.Expect(out).To(ContainSubstring("  - /metadata/labels/app.kubernetes.io~1name: must match regex '(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?'"))
	g.Expect(out).To(ContainSubstring("  - /metadata/labels/app.kubernetes.io~1instance: must be a string, got null"))
	g.Expect(out).To(ContainSubstring(`  - /metadata/annotations/_app.kubernetes.io~1name: key: must match regex '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'`))

	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-labels is invalid: yaml parse error"))
	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/duplicate-fields is invalid: yaml parse error"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 8: key "app" already set in map$`))
	g.Expect(out).To(MatchRegexp(`(?m)^  - line 11: key "tag" already set in map$`))

	g.Expect(out).To(ContainSubstring(invalidPath + " - OCIRepository/default/#4 is invalid: schema violation"))
	g.Expect(out).To(MatchRegexp(`(?m)^  - /metadata: missing property 'name' or 'generateName'$`))

	g.Expect(out).To(ContainSubstring("Summary: 4 resources found in 1 file - Valid: 0, Invalid: 4, Skipped: 0"))
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
	g.Expect(out).To(ContainSubstring("SealedSecret/default/minio-bucket-secret is skipped"))
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
		"--skip-kind", "SealedSecret",
		"--skip-kind", "source.toolkit.fluxcd.io/v1/GitRepository",
		"--skip-kind", "helm.toolkit.fluxcd.io/v2/HelmRelease",
		"--verbose",
	})
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(out).To(ContainSubstring(sourcesPath + " - SealedSecret/default/minio-bucket-secret is skipped: kind skipped"))
	g.Expect(out).To(ContainSubstring(" - GitRepository/"))
	g.Expect(out).To(ContainSubstring("is skipped: kind skipped"))
	g.Expect(out).To(ContainSubstring(" - HelmRelease/"))
}

// TestValidateCmd_SkipFile_DefaultHidesDotfiles verifies that without an
// explicit --skip-file the walker hides dotfiles and dot-directories so a
// .git/ alongside manifests doesn't trip schema resolution.
func TestValidateCmd_SkipFile_DefaultHidesDotfiles(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)
	writeManifest(t, manifestDir, ".hidden.yaml", invalidWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

// TestValidateCmd_SkipFile_CustomPattern verifies an explicit --skip-file
// replaces the default and matches by basename glob.
func TestValidateCmd_SkipFile_CustomPattern(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)
	writeManifest(t, manifestDir, "kustomization.yaml", invalidWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-file", "kustomization.yaml",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
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
	g.Expect(out).To(ContainSubstring("is invalid: schema violation"))
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

// TestValidateCmd_SkipJSONPath strips the SOPS metadata block from the
// encrypted Secret in the fixture so the strict v1.Secret schema accepts it.
// The unencrypted Secret in the same file is unaffected.
func TestValidateCmd_SkipJSONPath(t *testing.T) {
	g := NewWithT(t)

	secretsPath := "./testdata/validate/manifests/valid-secrets.yaml"

	// Without the strip, the SOPS Secret fails on additionalProperties.
	out, err := executeCommand([]string{
		"validate",
		secretsPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Secret/default/sops-secret is invalid: schema violation"))
	g.Expect(out).To(ContainSubstring("additional properties 'sops' not allowed"))

	// With the kind-scoped strip both Secrets validate.
	out, err = executeCommand([]string{
		"validate",
		secretsPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--skip-json-path", "Secret:/sops",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 2 resources found in 1 file - Valid: 2, Invalid: 0, Skipped: 0"))
}

func TestValidateCmd_SkipJSONPath_Invalid(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	_, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-json-path", "no-leading-slash",
	})
	g.Expect(err).To(MatchError(ContainSubstring("skip JSON path pattern")))
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

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  verbose: true
  skipKind:
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

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
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

// Config file's skip-json-path entries are picked up when the flag is absent.
func TestValidateCmd_Config_SkipJSONPath(t *testing.T) {
	g := NewWithT(t)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipJSONPath:
    - Secret:/sops
`)
	out, err := executeCommand([]string{
		"validate", "./testdata/validate/manifests/valid-secrets.yaml",
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 2, Invalid: 0, Skipped: 0"))
}

// CLI --skip-json-path replaces (does not merge with) the config's list.
// Mirrors TestValidateCmd_Config_CLIOverridesSlice for skip-kind.
func TestValidateCmd_Config_CLIOverridesSkipJSONPath(t *testing.T) {
	g := NewWithT(t)

	// Config strips /sops; CLI replaces the list with a different pointer
	// that doesn't match anything in the doc, so SOPS validation fails.
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipJSONPath:
    - Secret:/sops
`)
	_, err := executeCommand([]string{
		"validate", "./testdata/validate/manifests/valid-secrets.yaml",
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--config", cfg,
		"--skip-json-path", "Secret:/does-not-exist",
	})
	g.Expect(err).To(HaveOccurred())
}

// Config file's skip-file entries replace the built-in default and are
// picked up when the flag is absent on the CLI.
func TestValidateCmd_Config_SkipFile(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)
	writeManifest(t, manifestDir, "kustomization.yaml", invalidWidget)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipFile:
    - kustomization.yaml
`)
	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

// CLI --skip-kind replaces (does not merge with) the config's skip-kind list.
func TestValidateCmd_Config_CLIOverridesSlice(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipKind:
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

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  verbose: true
`)
	_, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())

	cfgZero := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
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

func TestValidateCmd_Config_EmptyValidateSection(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate: {}
`)
	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1"))
}

func TestValidateCmd_Config_APIVersionMissing(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `validate:
  verbose: true
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring(`unsupported apiVersion ""`)))
}

func TestValidateCmd_Config_APIVersionUnsupported(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v2
kind: Config
validate:
  verbose: true
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring(`unsupported apiVersion "schema.plugin.fluxcd.io/v2"`)))
}

func TestValidateCmd_Config_StrictUnknownKey(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skip-kind:
    - Secret
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("skip-kind"))
}

func TestValidateCmd_Config_StrictUnknownSection(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
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

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipKind:
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

// When neither --config nor FLUX_SCHEMA_CONFIG is set, the CLI looks for a
// config file next to the executable with a .config suffix.
func TestValidateCmd_Config_ExecutableDefault(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	exe := filepath.Join(t.TempDir(), "flux-schema")
	cfg := exe + ".config"
	g.Expect(os.WriteFile(cfg, []byte(`apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipKind:
    - Widget
`), 0o644)).To(Succeed())

	orig := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = orig })
	t.Setenv(envConfigFile, "")

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

	// Env var config has unsupported apiVersion — must not be read.
	envCfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v99
kind: Config`)
	t.Setenv("FLUX_SCHEMA_CONFIG", envCfg)

	flagCfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipKind:
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
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml",
		"apiVersion: schema.plugin.fluxcd.io/v1beta1\nkind: Config\nvalidate:\n\tverbose: true\n") // tabs are illegal indent
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

	g.Expect(expand([]string{"ecosystem"})).
		To(Equal([]string{validator.EcosystemSchemaLocation}))

	g.Expect(expand([]string{"ECOSYSTEM", "Ecosystem"})).
		To(Equal([]string{validator.EcosystemSchemaLocation, validator.EcosystemSchemaLocation}))

	g.Expect(expand([]string{"default", "ecosystem"})).
		To(Equal([]string{validator.DefaultSchemaLocation, validator.EcosystemSchemaLocation}))

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

// decodeReport parses the envelope emitted by --output json and returns the
// inner body for convenient assertions.
func decodeReport(t *testing.T, raw string) apiv1.Report {
	t.Helper()
	var env apiv1.Report
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode report: %v\nraw: %s", err, raw)
	}
	return env
}

func validateReportSchema(t *testing.T, raw string) {
	t.Helper()
	g := NewWithT(t)

	var doc any
	g.Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

	abs, err := filepath.Abs(filepath.Join("..", "..", "docs", "report-v1beta1.json"))
	g.Expect(err).ToNot(HaveOccurred())

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	schema, err := compiler.Compile("file://" + filepath.ToSlash(abs))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(schema.Validate(doc)).To(Succeed())
}

func validateConfigSchema(t *testing.T, raw string) {
	t.Helper()
	g := NewWithT(t)

	var doc any
	g.Expect(k8syaml.Unmarshal([]byte(raw), &doc)).To(Succeed())

	abs, err := filepath.Abs(filepath.Join("..", "..", "docs", "config-v1beta1.json"))
	g.Expect(err).ToNot(HaveOccurred())

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	schema, err := compiler.Compile("file://" + filepath.ToSlash(abs))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(schema.Validate(doc)).To(Succeed())
}

func TestValidateCmd_Config_Schema(t *testing.T) {
	validateConfigSchema(t, `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schemaLocation:
    - default
  skipKind:
    - Widget
  skipJSONPath:
    - Secret:/sops
  skipFile:
    - '.*'
  skipCELRules: true
  skipMissingSchemas: true
  verbose: true
  failFast: true
  concurrent: 8
  insecureSkipTLSVerify: true
  output: json
`)
}

func TestValidateCmd_Output_JSON_ValidManifest(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	path := writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).ToNot(HaveOccurred())

	validateReportSchema(t, out)
	env := decodeReport(t, out)
	g.Expect(env.APIVersion).To(Equal(apiv1.GroupVersion.String()))
	g.Expect(env.Kind).To(Equal(apiv1.ReportKind))
	g.Expect(env.Schema).To(Equal(apiv1.ReportSchema))
	g.Expect(env.Report.Reporter).To(HavePrefix("flux-schema/"))
	g.Expect(env.Report.Timestamp).ToNot(BeEmpty())
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Valid: 1, Invalid: 0, Skipped: 0}))
	g.Expect(env.Report.Results).To(HaveLen(1))

	res := env.Report.Results[0]
	g.Expect(res.Source).To(Equal(path))
	g.Expect(res.Idx).To(Equal(1))
	g.Expect(res.Status).To(Equal("valid"))
	g.Expect(res.Reason).To(BeEmpty())
	g.Expect(res.Violations).To(BeEmpty())
	g.Expect(res.Resource).ToNot(BeNil())
	g.Expect(*res.Resource).To(Equal(apiv1.ReportResource{
		APIVersion: "example.com/v1",
		Kind:       "Widget",
		Namespace:  "default",
		Name:       "ok-widget",
	}))

	// `reason` and `violations` must be omitted from the wire form of a valid
	// result so consumers can branch on presence.
	g.Expect(out).ToNot(ContainSubstring(`"reason"`))
	g.Expect(out).ToNot(ContainSubstring(`"violations"`))
}

func TestValidateCmd_Output_JSON_SchemaViolation(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "bad.yaml", invalidWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Valid: 0, Invalid: 1, Skipped: 0}))
	g.Expect(env.Report.Results).To(HaveLen(1))
	res := env.Report.Results[0]
	g.Expect(res.Status).To(Equal("invalid"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonSchemaViolation)))
	g.Expect(res.Violations).ToNot(BeEmpty())
	g.Expect(res.Violations[0].Path).To(Equal("/spec/name"))
	g.Expect(res.Violations[0].Message).ToNot(BeEmpty())
}

func TestValidateCmd_Output_JSON_MissingAPIVersionKind(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "noheader.yaml", `metadata:
  name: orphan
spec:
  name: x
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred())

	validateReportSchema(t, out)
	env := decodeReport(t, out)
	g.Expect(env.Report.Results).To(HaveLen(1))
	res := env.Report.Results[0]
	g.Expect(res.Status).To(Equal("invalid"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonSchemaViolation)))
	paths := make([]string, 0, len(res.Violations))
	for _, v := range res.Violations {
		paths = append(paths, v.Path)
	}
	g.Expect(paths).To(ConsistOf("/apiVersion", "/kind"))
	for _, v := range res.Violations {
		g.Expect(v.Message).To(Equal("missing required property"))
	}
}

func TestValidateCmd_Output_JSON_KindSkipped(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-kind", "Widget",
		"-o", "json",
	})
	g.Expect(err).ToNot(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Valid: 0, Invalid: 0, Skipped: 1}))
	g.Expect(env.Report.Results).To(HaveLen(1))
	res := env.Report.Results[0]
	g.Expect(res.Status).To(Equal("skipped"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonKindSkipped)))
	g.Expect(res.Violations).To(BeEmpty())
}

func TestValidateCmd_Output_JSON_SchemaNotFound(t *testing.T) {
	g := NewWithT(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-missing-schemas",
		"-o", "json",
	})
	g.Expect(err).ToNot(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Valid: 0, Invalid: 0, Skipped: 1}))
	res := env.Report.Results[0]
	g.Expect(res.Status).To(Equal("skipped"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonSchemaNotFound)))
	g.Expect(res.Violations).To(HaveLen(1))
	g.Expect(res.Violations[0].Path).To(BeEmpty())
	g.Expect(res.Violations[0].Message).To(ContainSubstring(`no schema for kind "Widget"`))
}

func TestValidateCmd_Output_JSON_SourceLoadError(t *testing.T) {
	g := NewWithT(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	out, err := executeCommand([]string{
		"validate", missing,
		"--schema-location", filepath.Join(t.TempDir(), "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred())

	validateReportSchema(t, out)
	env := decodeReport(t, out)
	g.Expect(env.Report.Results).To(HaveLen(1))
	res := env.Report.Results[0]
	g.Expect(res.Resource).To(BeNil())
	g.Expect(res.Status).To(Equal("invalid"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonSourceLoadError)))
	g.Expect(res.Violations).To(HaveLen(1))
	g.Expect(res.Violations[0].Message).To(ContainSubstring("no such file or directory"))

	// `"resource": null` must appear on the wire so consumers can distinguish
	// "no identity" from an empty object.
	g.Expect(out).To(ContainSubstring(`"resource": null`))
}

func TestValidateCmd_Output_YAML_SmokeTest(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "yaml",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("apiVersion: schema.plugin.fluxcd.io/v1beta1"))
	g.Expect(out).To(ContainSubstring("kind: Report"))
	g.Expect(out).To(ContainSubstring("reporter: flux-schema/"))
	g.Expect(out).To(ContainSubstring("status: valid"))
	g.Expect(out).ToNot(ContainSubstring("version: 1.0.0"))
	// $schema is a JSON-only pointer; dropping it keeps YAML output clean
	// for consumers like yq that don't care about the envelope schema URL.
	g.Expect(out).ToNot(ContainSubstring("$schema"))

	var env apiv1.Report
	// sigs.k8s.io/yaml re-encodes through JSON; json.Unmarshal is not
	// appropriate here. Use sigs.k8s.io/yaml.Unmarshal for parity.
	g.Expect(k8syaml.Unmarshal([]byte(out), &env)).To(Succeed())
	g.Expect(env.APIVersion).To(Equal(apiv1.GroupVersion.String()))
	g.Expect(env.Kind).To(Equal(apiv1.ReportKind))
	g.Expect(env.Schema).To(BeEmpty())
	g.Expect(env.Report.Summary.Valid).To(Equal(1))
}

// JSON/YAML output always emits every result regardless of --verbose; the
// structured form is for machines, and filtering belongs downstream.
func TestValidateCmd_Output_JSON_IgnoresVerbose(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "mixed.yaml", validWidget+"---\n"+invalidWidget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 2, Valid: 1, Invalid: 1, Skipped: 0}))
	g.Expect(env.Report.Results).To(HaveLen(2))

	statuses := []string{env.Report.Results[0].Status, env.Report.Results[1].Status}
	g.Expect(statuses).To(ConsistOf("valid", "invalid"))
}

func TestValidateCmd_Output_JSON_Config(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractWidgetSchema(t)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validWidget)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  output: json
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.APIVersion).To(Equal(apiv1.GroupVersion.String()))
	g.Expect(env.Kind).To(Equal(apiv1.ReportKind))
	g.Expect(env.Report.Summary.Valid).To(Equal(1))
}

func TestValidateCmd_Output_Unsupported(t *testing.T) {
	g := NewWithT(t)
	_, err := executeCommand([]string{"validate", "--output", "toml"})
	g.Expect(err).To(MatchError(ContainSubstring("unsupported output format")))
}

func TestValidateCmd_Config_InvalidOutput(t *testing.T) {
	g := NewWithT(t)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  output: toml
`)
	_, err := executeCommand([]string{"validate", "--config", cfg})
	g.Expect(err).To(MatchError(ContainSubstring("unsupported output format")))
}

// celGadgetCRDYAML defines a CRD whose spec carries an x-kubernetes-validations
// rule: spec.mode must equal "ok". A separate Kind keeps these schemas out of
// the cache shared by the Widget tests above.
const celGadgetCRDYAML = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gadgets.example.com
spec:
  group: example.com
  names:
    kind: Gadget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                mode:
                  type: string
              x-kubernetes-validations:
                - rule: "self.mode == 'ok'"
                  message: "spec.mode must be ok"
`

const validGadget = `apiVersion: example.com/v1
kind: Gadget
metadata:
  name: ok-gadget
  namespace: default
spec:
  mode: ok
`

const celViolatingGadget = `apiVersion: example.com/v1
kind: Gadget
metadata:
  name: bad-gadget
  namespace: default
spec:
  mode: nope
`

func TestValidateCmd_CELRule_Invalid(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractCRDSchema(t, celGadgetCRDYAML)
	manifestDir := t.TempDir()
	path := writeManifest(t, manifestDir, "bad.yaml", celViolatingGadget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred())

	env := decodeReport(t, out)
	g.Expect(env.Report.Summary).To(Equal(apiv1.ReportSummary{Total: 1, Valid: 0, Invalid: 1, Skipped: 0}))
	g.Expect(env.Report.Results).To(HaveLen(1))
	res := env.Report.Results[0]
	g.Expect(res.Source).To(Equal(path))
	g.Expect(res.Status).To(Equal("invalid"))
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonCELViolation)))
	g.Expect(res.Violations).ToNot(BeEmpty())
	g.Expect(res.Violations[0].Path).To(Equal("/spec"))
	g.Expect(res.Violations[0].Message).To(ContainSubstring("spec.mode must be ok"))
}

func TestValidateCmd_CELRule_SkipFlag(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractCRDSchema(t, celGadgetCRDYAML)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "bad.yaml", celViolatingGadget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-cel-rules",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

func TestValidateCmd_CELRule_ValidPassesByDefault(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractCRDSchema(t, celGadgetCRDYAML)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "ok.yaml", validGadget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

func TestValidateCmd_CELRule_ConfigFile(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractCRDSchema(t, celGadgetCRDYAML)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "bad.yaml", celViolatingGadget)
	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  skipCELRules: true
`)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--config", cfg,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Valid: 1, Invalid: 0"))
}

// TestValidateCmd_CELRule_CatalogHelmRelease exercises a real catalog rule
// against the checked-in HelmRelease v2 schema: a HelmRelease that sets
// both `chart` and `chartRef` violates the spec-level CEL rule
// `(has(self.chart) && !has(self.chartRef)) || (!has(self.chart) && has(self.chartRef))`.
func TestValidateCmd_CELRule_CatalogHelmRelease(t *testing.T) {
	g := NewWithT(t)

	manifestPath := "./testdata/validate/manifests/invalid-cel.yaml"

	out, err := executeCommand([]string{
		"validate", manifestPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(out).To(ContainSubstring("HelmRelease/apps/webapp is invalid: cel violation"))
	g.Expect(out).To(ContainSubstring("/spec: Invalid value: either chart or chartRef must be set"))

	// --skip-cel-rules disables the check; same fixture validates clean.
	out, err = executeCommand([]string{
		"validate", manifestPath,
		"--schema-location", "./testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json",
		"--skip-cel-rules",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("Summary: 1 resource found in 1 file - Valid: 1, Invalid: 0, Skipped: 0"))
}

// TestValidateCmd_CELRule_SkipJSONPath documents that --skip-json-path strips
// fields BEFORE CEL evaluation. The CEL rule "spec.mode == 'ok'" inspects
// spec.mode; stripping it makes `has(self.mode)` semantics matter — the rule
// here references the field directly via self.mode, which becomes an error
// when the field is absent. The test asserts the interaction is observable
// (CEL sees the stripped doc) rather than what passes — the value of the
// fixture is documenting the behavior, not the specific outcome.
func TestValidateCmd_CELRule_SkipJSONPath(t *testing.T) {
	g := NewWithT(t)
	schemaDir := extractCRDSchema(t, celGadgetCRDYAML)
	manifestDir := t.TempDir()
	writeManifest(t, manifestDir, "bad.yaml", celViolatingGadget)

	out, err := executeCommand([]string{
		"validate", manifestDir,
		"--schema-location", filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
		"--skip-json-path", "Gadget:/spec/mode",
		"-o", "json",
	})
	g.Expect(err).To(HaveOccurred()) // stripped self.mode -> CEL reference failure

	env := decodeReport(t, out)
	res := env.Report.Results[0]
	g.Expect(res.Reason).To(Equal(apiv1.ReportReason(validator.ReasonCELViolation)))
	g.Expect(res.Violations[0].Message).ToNot(BeEmpty())
}
