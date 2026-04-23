// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/tar"
)

func TestExtractCRDCmd_DefaultFormat(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{"extract", "crd", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())

	got, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(Equal(minimalCRDGolden))
}

func TestExtractCRDCmd_FlatFormat(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{
		"extract", "crd", input,
		"--output-dir", outDir,
		"--output-format", "{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json",
	})
	g.Expect(err).ToNot(HaveOccurred())

	got, err := os.ReadFile(filepath.Join(outDir, "widget-example-v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(Equal(minimalCRDGolden))
}

func TestExtractCRDCmd_AutoCreatesOutputDir(t *testing.T) {
	g := NewWithT(t)

	parent := t.TempDir()
	outDir := filepath.Join(parent, "does", "not", "exist")
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{"extract", "crd", input, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())

	_, err = os.Stat(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractCRDCmd_DefaultsOutputDirToCwd(t *testing.T) {
	g := NewWithT(t)

	t.Chdir(t.TempDir())
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{"extract", "crd", input})
	g.Expect(err).ToNot(HaveOccurred())

	_, err = os.Stat(filepath.Join("example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
}

func TestExtractCRDCmd_NonexistentInput(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	_, err := executeCommand([]string{"extract", "crd", filepath.Join(outDir, "missing.yaml"), "--output-dir", outDir})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("error(s) during extraction"))
}

func TestExtractCRDCmd_WritesArchive(t *testing.T) {
	g := NewWithT(t)

	archivePath := filepath.Join(t.TempDir(), "out.tar.gz")
	input := writeCRDFixture(t)

	out, err := executeCommand([]string{"extract", "crd", input, "--output-archive", archivePath})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("OK   " + input + " -> " + filepath.Join("example.com", "widget_v1.json")))
	g.Expect(out).To(ContainSubstring("wrote " + archivePath + " (1 schema(s))"))

	f, err := os.Open(archivePath)
	g.Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	extracted := t.TempDir()
	g.Expect(tar.Untar(f, extracted)).To(Succeed())

	got, err := os.ReadFile(filepath.Join(extracted, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(Equal(minimalCRDGolden))
}

func TestExtractCRDCmd_ArchiveCreatesParentDir(t *testing.T) {
	g := NewWithT(t)

	archivePath := filepath.Join(t.TempDir(), "does", "not", "exist", "out.tar.gz")
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{"extract", "crd", input, "--output-archive", archivePath})
	g.Expect(err).ToNot(HaveOccurred())

	info, err := os.Stat(archivePath)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(info.Size()).To(BeNumerically(">", 0))
}

func TestExtractCRDCmd_ArchiveRejectsBadExtension(t *testing.T) {
	g := NewWithT(t)

	input := writeCRDFixture(t)
	_, err := executeCommand([]string{
		"extract", "crd", input,
		"--output-archive", filepath.Join(t.TempDir(), "out.zip"),
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("must end in .tar.gz or .tgz"))
}

func TestExtractCRDCmd_ArchiveAndDirMutuallyExclusive(t *testing.T) {
	g := NewWithT(t)

	input := writeCRDFixture(t)
	_, err := executeCommand([]string{
		"extract", "crd", input,
		"--output-dir", t.TempDir(),
		"--output-archive", filepath.Join(t.TempDir(), "out.tar.gz"),
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("none of the others can be"))
}

func TestExtractCRDCmd_UnknownTemplateVar(t *testing.T) {
	g := NewWithT(t)

	outDir := t.TempDir()
	input := writeCRDFixture(t)

	_, err := executeCommand([]string{
		"extract", "crd", input,
		"--output-dir", outDir,
		"--output-format", "{{ .Unknown }}.json",
	})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("error(s) during extraction"))
}

// To refresh the testdata fixture and golden file, run:
//
//	kubectl get crd helmreleases.helm.toolkit.fluxcd.io -o yaml \
//	  > cmd/flux-schema/testdata/extract/helmrelease-helm-v2.yaml
//	make run GO_RUN_ARGS="extract crd \
//	  cmd/flux-schema/testdata/extract/helmrelease-helm-v2.yaml \
//	  --output-dir cmd/flux-schema/testdata/extract"
func TestExtractCRDCmd_HelmReleaseGolden(t *testing.T) {
	g := NewWithT(t)

	inputPath := filepath.Join("testdata", "extract", "helmrelease-helm-v2.yaml")
	goldenPath := filepath.Join("testdata", "extract", "helm.toolkit.fluxcd.io", "helmrelease_v2.json")

	outDir := t.TempDir()
	_, err := executeCommand([]string{"extract", "crd", inputPath, "--output-dir", outDir})
	g.Expect(err).ToNot(HaveOccurred())

	got, err := os.ReadFile(filepath.Join(outDir, "helm.toolkit.fluxcd.io", "helmrelease_v2.json"))
	g.Expect(err).ToNot(HaveOccurred())

	want, err := os.ReadFile(goldenPath)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(Equal(string(want)))
}

func TestExtractCRDCmd_StripDescription(t *testing.T) {
	g := NewWithT(t)

	fixture := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          description: top-level
          properties:
            spec:
              type: object
              description: spec block
              properties:
                name:
                  type: string
                  description: widget name
`
	tmp := t.TempDir()
	input := filepath.Join(tmp, "fixture.yaml")
	if err := os.WriteFile(input, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outDir := filepath.Join(tmp, "out")

	_, err := executeCommand([]string{"extract", "crd", input, "--output-dir", outDir, "--strip-description"})
	g.Expect(err).ToNot(HaveOccurred())

	data, err := os.ReadFile(filepath.Join(outDir, "example.com", "widget_v1.json"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).ToNot(ContainSubstring(`"description"`))
}

func writeCRDFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.yaml")
	if err := os.WriteFile(path, []byte(minimalCRDYAML), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

const minimalCRDYAML = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                name:
                  type: string
                port:
                  x-kubernetes-int-or-string: true
                values:
                  type: object
                  x-kubernetes-preserve-unknown-fields: true
`

const minimalCRDGolden = `{
  "properties": {
    "spec": {
      "additionalProperties": false,
      "properties": {
        "name": {
          "type": "string"
        },
        "port": {
          "oneOf": [
            {
              "type": "string"
            },
            {
              "type": "integer"
            }
          ]
        },
        "values": {
          "type": "object",
          "x-kubernetes-preserve-unknown-fields": true
        }
      },
      "type": "object"
    }
  },
  "type": "object"
}
`
