// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/tar"
)

const restrictionExample1 = `
apiVersion: my.group.io/v1alpha1
kind: MyKind
restriction: my-restriction

spec:
  foo: $string                  # required string
  baz: qux                      # const, optional
  replicas: 3                   # const, optional
  name: $string(pattern=^[a-z][a-z0-9-]*$)
  size: $enum[Small, Medium, Large]
  port: $integer(min=1, max=65535)
  description: $string?         # optional string
  legacy: $forbid               # must be absent
  $required: [baz]              # baz must be present
  config:
    $closed: true               # no other keys allowed
    enabled: $boolean
`

const restrictionExample2 = `
apiVersion: fluxcd.controlplane.io/v1
kind: ResourceSetInputProvider
restriction: app-bundle

metadata:
  name: $string
  namespace: $forbid

spec:
  type: Static
  defaultValues:
    tenant: $string
    app:
      version: $string
      replicas: 2
`

func writeRestrictionFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "restriction.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write restriction fixture: %v", err)
	}
	return p
}

func TestExtractRestrictionCmd_Example1(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeRestrictionFixture(t, restrictionExample1)

	output, err := executeCommand([]string{"extract", "restriction", input, "-d", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	expectedPath := filepath.Join(outDir, "my.group.io", "mykind_v1alpha1_my-restriction.json")
	g.Expect(output).To(ContainSubstring("OK   " + input + " -> " + expectedPath))

	data, err := os.ReadFile(expectedPath)
	g.Expect(err).ToNot(HaveOccurred())

	var schema map[string]any
	g.Expect(json.Unmarshal(data, &schema)).To(Succeed())
	g.Expect(schema["type"]).To(Equal("object"))

	props := schema["properties"].(map[string]any)
	g.Expect(props["apiVersion"]).To(Equal(map[string]any{"type": "string", "const": "my.group.io/v1alpha1"}))
	g.Expect(props["kind"]).To(Equal(map[string]any{"type": "string", "const": "MyKind"}))
	g.Expect(props).ToNot(HaveKey("restriction"))

	spec := props["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	g.Expect(specProps["foo"]).To(Equal(map[string]any{"type": "string"}))
	g.Expect(specProps["baz"]).To(Equal(map[string]any{"type": "string", "const": "qux"}))
	g.Expect(specProps["replicas"]).To(Equal(map[string]any{"type": "integer", "const": float64(3)}))
	g.Expect(specProps["legacy"]).To(BeFalse())

	g.Expect(spec["required"]).To(Equal([]any{"baz", "foo", "name", "port", "size"}))
	g.Expect(schema["required"]).To(Equal([]any{"apiVersion", "kind"}))
}

func TestExtractRestrictionCmd_Example2(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeRestrictionFixture(t, restrictionExample2)

	output, err := executeCommand([]string{"extract", "restriction", input, "-d", outDir})
	g.Expect(err).ToNot(HaveOccurred())
	expectedPath := filepath.Join(outDir, "fluxcd.controlplane.io", "resourcesetinputprovider_v1_app-bundle.json")
	g.Expect(output).To(ContainSubstring("OK   " + input + " -> " + expectedPath))

	data, err := os.ReadFile(expectedPath)
	g.Expect(err).ToNot(HaveOccurred())

	var schema map[string]any
	g.Expect(json.Unmarshal(data, &schema)).To(Succeed())

	props := schema["properties"].(map[string]any)
	meta := props["metadata"].(map[string]any)
	g.Expect(meta["required"]).To(Equal([]any{"name"}))
	metaProps := meta["properties"].(map[string]any)
	g.Expect(metaProps["namespace"]).To(BeFalse())

	spec := props["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	g.Expect(specProps["type"]).To(Equal(map[string]any{"type": "string", "const": "Static"}))

	defaultVals := specProps["defaultValues"].(map[string]any)
	g.Expect(defaultVals["required"]).To(Equal([]any{"tenant"}))
}

func TestExtractRestrictionCmd_CustomOutputFormat(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeRestrictionFixture(t, restrictionExample1)

	_, err := executeCommand([]string{
		"extract", "restriction", input,
		"-d", outDir,
		"--output-format", "{{ .Group }}/{{ .Kind }}_{{ .Restriction }}_{{ .Version }}.json",
	})
	g.Expect(err).ToNot(HaveOccurred())

	schemaPath := filepath.Join(outDir, "my.group.io", "mykind_my-restriction_v1alpha1.json")
	data, err := os.ReadFile(schemaPath)
	g.Expect(err).ToNot(HaveOccurred())

	var schema map[string]any
	g.Expect(json.Unmarshal(data, &schema)).To(Succeed())
	props := schema["properties"].(map[string]any)
	g.Expect(props["apiVersion"]).To(Equal(map[string]any{"type": "string", "const": "my.group.io/v1alpha1"}))
	g.Expect(props["kind"]).To(Equal(map[string]any{"type": "string", "const": "MyKind"}))
}

func TestExtractRestrictionCmd_OutputArchive(t *testing.T) {
	g := NewWithT(t)

	outArchive := filepath.Join(t.TempDir(), "schemas.tar.gz")
	input := writeRestrictionFixture(t, restrictionExample1)

	output, err := executeCommand([]string{
		"extract", "restriction", input,
		"-a", outArchive,
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(output).To(ContainSubstring("wrote " + outArchive + " (1 schema(s))"))

	f, err := os.Open(outArchive)
	g.Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	destDir := t.TempDir()
	g.Expect(tar.Untar(f, destDir)).To(Succeed())

	extracted := filepath.Join(destDir, "my.group.io", "mykind_v1alpha1_my-restriction.json")
	g.Expect(extracted).To(BeAnExistingFile())
}

func TestExtractRestrictionCmd_Stdin(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	replaceStdin(t, restrictionExample1)

	output, err := executeCommand([]string{
		"extract", "restriction", "-",
		"-d", outDir,
	})
	g.Expect(err).ToNot(HaveOccurred())
	expectedPath := filepath.Join(outDir, "my.group.io", "mykind_v1alpha1_my-restriction.json")
	g.Expect(output).To(ContainSubstring("OK   stdin -> " + expectedPath))

	g.Expect(expectedPath).To(BeAnExistingFile())
}

func TestExtractRestrictionCmd_WithFieldIndex(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeRestrictionFixture(t, restrictionExample1)

	_, err := executeCommand([]string{
		"extract", "restriction", input,
		"-d", outDir,
		"--with-field-index",
	})
	g.Expect(err).ToNot(HaveOccurred())

	indexPath := filepath.Join(outDir, "my.group.io", "mykind_v1alpha1_my-restriction.fields.txt")
	g.Expect(indexPath).To(BeAnExistingFile())
}

func TestExtractRestrictionCmd_ErrorCases(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	missingAPIVersion := writeRestrictionFixture(t, `
kind: MyKind
restriction: my-restr
spec:
  foo: bar
`)
	_, err := executeCommand([]string{"extract", "restriction", missingAPIVersion, "-d", outDir})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("1 error(s) during extraction"))
}
