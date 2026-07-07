// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/flux-schema/internal/validator"
)

func TestExplainCmd_Resource(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "pods",
		"--api-version", "v1",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(Equal("KIND:       Pod\n" +
		"VERSION:    v1\n" +
		"\n" +
		"DESCRIPTION:\n" +
		"    Pod is a collection of containers that can run on a host. This resource is\n" +
		"    created by clients and scheduled onto hosts.\n" +
		"    \n" +
		"FIELDS:\n" +
		"  apiVersion\t<string>\n" +
		"    APIVersion defines the versioned schema of this representation of an object.\n" +
		"\n" +
		"  kind\t<string>\n" +
		"    Kind is a string value representing the REST resource this object\n" +
		"    represents.\n" +
		"\n" +
		"  metadata\t<ObjectMeta>\n" +
		"    Standard object's metadata.\n" +
		"\n" +
		"  spec\t<PodSpec>\n" +
		"    Specification of the desired behavior of the pod.\n" +
		"\n" +
		"  status\t<PodStatus>\n" +
		"    Most recently observed status of the pod.\n" +
		"\n" +
		"\n"))
}

func TestExplainCmd_Field(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "pods.spec.containers.imagePullPolicy",
		"--api-version", "v1",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(Equal("KIND:       Pod\n" +
		"VERSION:    v1\n" +
		"\n" +
		"FIELD: imagePullPolicy <string>\n" +
		"ENUM:\n" +
		"    Always\n" +
		"    IfNotPresent\n" +
		"    Never\n" +
		"\n" +
		"DESCRIPTION:\n" +
		"    Image pull policy. One of Always, Never, IfNotPresent.\n" +
		"    \n" +
		"\n"))
}

func TestExplainCmd_OpenAPIV2Output(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "pods.spec",
		"--api-version", "v1",
		"--output", "plaintext-openapiv2",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(HavePrefix("KIND:     Pod\n" +
		"VERSION:  v1\n" +
		"\n" +
		"RESOURCE: spec <PodSpec>\n" +
		"\n" +
		"DESCRIPTION:\n" +
		"     Specification of the desired behavior of the pod.\n" +
		"\n" +
		"FIELDS:\n" +
		"   containers\t<[]Container> -required-\n"))
}

func TestExplainCmd_Recursive(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "pods",
		"--api-version", "v1",
		"--recursive",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("  metadata\t<ObjectMeta>\n    name\t<string>\n"))
	g.Expect(out).To(ContainSubstring("  spec\t<PodSpec>\n    containers\t<[]Container> -required-\n      image\t<string>\n      imagePullPolicy\t<string>\n      enum: Always, IfNotPresent, Never\n"))
}

func TestExplainCmd_FieldIndexMetadataFallback(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "helmreleases",
		"--api-version", "helm.toolkit.fluxcd.io/v2",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("GROUP:      helm.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out).To(ContainSubstring("VERSION:    v2\n"))
}

func TestExplainCmd_QualifiedResource(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "helmreleases.helm.toolkit.fluxcd.io.spec",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("GROUP:      helm.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out).To(ContainSubstring("VERSION:    v2\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <Object>\n"))
}

func TestExplainCmd_UnqualifiedCRDReferences(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "OCIRepository.spec.verify",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("GROUP:      source.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring("KIND:       OCIRepository\n"))
	g.Expect(out).To(ContainSubstring("FIELD: verify <OCIRepositoryVerification>\n"))

	out, err = executeCommand([]string{
		"explain", "ocirepo.spec.verify",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("GROUP:      source.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring("KIND:       OCIRepository\n"))
	g.Expect(out).To(ContainSubstring("FIELD: verify <OCIRepositoryVerification>\n"))
}

func TestExplainCmd_ShortNames(t *testing.T) {
	g := NewWithT(t)

	out, err := executeCommand([]string{
		"explain", "po.spec",
		"--api-version", "v1",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("KIND:       Pod\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <PodSpec>\n"))

	out, err = executeCommand([]string{
		"explain", "hr.spec",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <Object>\n"))

	out, err = executeCommand([]string{
		"explain", "hr.spec",
		"--api-version", "helm.toolkit.fluxcd.io/v2",
		"--schema-location", filepath.Join("testdata", "explain", "catalog"),
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <Object>\n"))
}

func TestExplainCmd_CompleteResourceReferences(t *testing.T) {
	g := NewWithT(t)
	catalog := filepath.Join("testdata", "explain", "catalog")

	out, err := executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "o"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("ocirepositories.source.toolkit.fluxcd.io\n"))
	g.Expect(out).ToNot(ContainSubstring("OCIRepository\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "oci"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("ocirepositories.source.toolkit.fluxcd.io\n"))
	g.Expect(out).ToNot(ContainSubstring("OCIRepository\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "ocirepo.source"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("ocirepositories.source.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "OCIRepository.source"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("ocirepositories.source.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "h"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("helmreleases.helm.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "hr"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("helmreleases.helm.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "hr.helm"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("helmreleases.helm.toolkit.fluxcd.io\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "p"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("pods\n"))
	g.Expect(out).To(ContainSubstring(":4"))

	out, err = executeCommand([]string{"__complete", "explain", "--schema-location", catalog, "po"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("pods\n"))
	g.Expect(out).To(ContainSubstring(":4"))
}

func TestExplainCmd_ConfigExecutableDefault(t *testing.T) {
	g := NewWithT(t)

	exe := filepath.Join(t.TempDir(), "flux-schema")
	cfg := exe + ".config"
	catalog := filepath.Join("testdata", "explain", "catalog")
	g.Expect(os.WriteFile(cfg, []byte(`apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
explain:
  schemaLocation:
    - `+catalog+`
  apiVersion: v1
`), 0o644)).To(Succeed())

	orig := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = orig })
	t.Setenv(envConfigFile, "")

	out, err := executeCommand([]string{"explain", "po.spec"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("KIND:       Pod\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <PodSpec>\n"))
}

func TestExplainCmd_ConfigFlag(t *testing.T) {
	g := NewWithT(t)

	cfg := writeManifest(t, t.TempDir(), ".fluxschema.yml", `apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
explain:
  schemaLocation:
    - testdata/explain/catalog
  apiVersion: helm.toolkit.fluxcd.io/v2
`)

	out, err := executeCommand([]string{"explain", "hr.spec", "--config", cfg})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(out).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out).To(ContainSubstring("FIELD: spec <Object>\n"))
}

func TestExplainCmd_MissingDefaultConfig(t *testing.T) {
	g := NewWithT(t)

	exe := filepath.Join(t.TempDir(), "flux-schema")
	orig := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = orig })
	t.Setenv(envConfigFile, "")

	_, err := executeCommand([]string{"explain", "pods"})
	g.Expect(err).To(MatchError(ContainSubstring("read " + exe + ".config")))
}

func TestExplainSchemaLocationDefaultExpansion(t *testing.T) {
	g := NewWithT(t)
	defer resetCmdArgs()

	explainArgs.schemaLocations = []string{"default", "ecosystem", "./catalog"}
	locations, err := buildExplainSchemaLocations()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(locations).To(Equal([]string{
		"https://raw.githubusercontent.com/fluxcd/flux-schema/main/catalog/latest/{{.Group}}/{{.Kind}}_{{.Version}}.json",
		validator.EcosystemSchemaLocation,
		"./catalog/{{.Group}}/{{.Kind}}_{{.Version}}.json",
	}))
	g.Expect(buildExplainMetadataLocations(locations)).To(Equal([]string{
		"https://raw.githubusercontent.com/fluxcd/flux-schema/main/catalog/latest/.explain",
		"./catalog/.explain",
	}))
	g.Expect(buildExplainIndexLocations(locations)).To(Equal([]string{validator.EcosystemIndexLocation}))
}
