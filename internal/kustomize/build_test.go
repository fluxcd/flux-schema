// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package kustomize

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestBuild(t *testing.T) {
	for _, tt := range []struct {
		dir     string
		names   []string
		origins []string
	}{
		{
			dir:     "simple",
			names:   []string{"settings", "apps"},
			origins: []string{"testdata/simple/resources.yaml", "testdata/simple/resources.yaml"},
		},
		{
			dir:     "overlay",
			names:   []string{"demo-settings", "apps", "demo-generated"},
			origins: []string{"testdata/simple/resources.yaml", "testdata/simple/resources.yaml", ""},
		},
		{
			dir:     "external",
			names:   []string{"outside"},
			origins: []string{"testdata/outside.yaml"},
		},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			g := NewWithT(t)
			dir := filepath.Join("testdata", tt.dir)
			file, ok := Detect(dir)
			g.Expect(ok).To(BeTrue())
			before, err := os.ReadFile(file)
			g.Expect(err).NotTo(HaveOccurred())

			docs, err := Build(dir)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(docs).To(HaveLen(len(tt.names)))
			for i, doc := range docs {
				node, err := yaml.Parse(string(doc.Raw))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(node.GetName()).To(Equal(tt.names[i]))
				g.Expect(doc.Origin).To(Equal(filepath.FromSlash(tt.origins[i])))
				g.Expect(node.GetAnnotations()).NotTo(HaveKey("config.kubernetes.io/origin"))
				if node.GetKind() == "Namespace" || node.GetName() == "demo-generated" || tt.dir == "external" {
					annotations, err := node.Pipe(yaml.Lookup("metadata", "annotations"))
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(annotations).To(BeNil())
				}
				if tt.dir == "overlay" {
					g.Expect(node.GetLabels()).To(HaveKeyWithValue("example.com/component", "included"))
					g.Expect(node.GetLabels()).To(HaveKey("app.kubernetes.io/managed-by"))
					if node.GetKind() != "Namespace" {
						g.Expect(node.GetNamespace()).To(Equal("apps"))
					}
					if node.GetName() == "demo-settings" {
						g.Expect(node.GetAnnotations()).To(Equal(map[string]string{"example.com/keep": "true"}))
						mode, err := node.Pipe(yaml.Lookup("data", "mode"))
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(mode.YNode().Value).To(Equal("patched"))
					}
				}
			}
			again, err := Build(dir)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(again).To(Equal(docs))
			after, err := os.ReadFile(file)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(after).To(Equal(before))
		})
	}
}

func TestBuildErrors(t *testing.T) {
	for _, tt := range []struct {
		dir string
		err string
	}{
		{dir: "broken", err: "missing.yaml"},
		{dir: "component", err: "no matches"},
		{dir: "plugin", err: "external plugins disabled"},
		{dir: ".", err: "no kustomization file"},
		{dir: "missing", err: "not a valid directory"},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			g := NewWithT(t)
			docs, err := Build(filepath.Join("testdata", tt.dir))
			g.Expect(err).To(MatchError(ContainSubstring(tt.err)))
			g.Expect(docs).To(BeNil())
		})
	}
}

func TestBuildKustomizationParsing(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":           "",
		"comments":        "# no kustomization\n",
		"scalar":          "not-a-kustomization",
		"sequence":        "- resources: []\n",
		"duplicate field": "resources: []\nresources: []\n",
		"unknown field":   "resources: []\nnotAField: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			g.Expect(os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(raw), 0o644)).To(Succeed())
			res, buildErr := build(filesys.MakeFsOnDisk(), dir)
			docs, err := Build(dir)
			if buildErr != nil {
				g.Expect(err).To(HaveOccurred())
				g.Expect(docs).To(BeNil())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(docs).To(HaveLen(res.Size()))
			}
		})
	}
}

func TestOriginFS(t *testing.T) {
	for _, tt := range []struct {
		name     string
		raw      string
		options  []string
		injected bool
	}{
		{name: "absent", raw: "resources: []", options: []string{"originAnnotations"}, injected: true},
		{name: "present", raw: "buildMetadata: [originAnnotations]", options: []string{"originAnnotations"}},
		{
			name: "preserve options", raw: "buildMetadata: [managedByLabel]",
			options: []string{"managedByLabel", "originAnnotations"}, injected: true,
		},
		{name: "invalid metadata passed through", raw: "buildMetadata: not-a-list"},
		{name: "invalid yaml passed through", raw: "resources: ["},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fs := filesys.MakeFsInMemory()
			g.Expect(fs.WriteFile("/kustomization.yaml", []byte(tt.raw))).To(Succeed())
			g.Expect(fs.Mkdir("/base")).To(Succeed())
			g.Expect(fs.WriteFile("/base/kustomization.yaml", []byte(tt.raw))).To(Succeed())
			wrapped := &originFS{FileSystem: fs, rootFile: "/kustomization.yaml"}
			raw, err := wrapped.ReadFile("/kustomization.yaml")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(wrapped.injected).To(Equal(tt.injected))
			if tt.options == nil {
				g.Expect(string(raw)).To(Equal(tt.raw))
			} else {
				node, err := yaml.Parse(string(raw))
				g.Expect(err).NotTo(HaveOccurred())
				metadata, err := node.Pipe(yaml.Lookup("buildMetadata"))
				g.Expect(err).NotTo(HaveOccurred())
				elements, err := metadata.Elements()
				g.Expect(err).NotTo(HaveOccurred())
				values := make([]string, len(elements))
				for i, elem := range elements {
					values[i] = elem.YNode().Value
				}
				g.Expect(values).To(Equal(tt.options))
			}
			raw, err = wrapped.ReadFile("/base/kustomization.yaml")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(raw)).To(Equal(tt.raw))
		})
	}
}

func TestBuildInvalidKustomizationError(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: [\n"), 0o644)).To(Succeed())
	_, err := Build(dir)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).NotTo(ContainSubstring("unable to find one of"))
}

func TestBuildKeepsRequestedOriginAnnotations(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
		[]byte("buildMetadata: [originAnnotations]\nresources: [cm.yaml]\n"), 0o644)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n"), 0o644)).To(Succeed())
	docs, err := Build(dir)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(docs).To(HaveLen(1))
	g.Expect(docs[0].Origin).To(Equal(filepath.Join(dir, "cm.yaml")))
	node, err := yaml.Parse(string(docs[0].Raw))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(node.GetAnnotations()).To(HaveKeyWithValue("config.kubernetes.io/origin", "path: cm.yaml\n"))
}

func TestOriginPath(t *testing.T) {
	for _, tt := range []struct {
		name   string
		origin *resource.Origin
		want   string
	}{
		{name: "absent"},
		{name: "generated", origin: &resource.Origin{ConfiguredIn: "kustomization.yaml"}},
		{name: "local", origin: &resource.Origin{Path: "../base/app.yaml"}, want: "apps/base/app.yaml"},
		{
			name:   "remote",
			origin: &resource.Origin{Path: "base/app.yaml", Repo: "https://example.com/org/repo", Ref: "release/v1"},
			want:   "https://example.com/org/repo//base/app.yaml?ref=release%2Fv1",
		},
		{
			name:   "remote default ref",
			origin: &resource.Origin{Path: "app.yaml", Repo: "https://example.com/org/repo"},
			want:   "https://example.com/org/repo//app.yaml",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(originPath("apps/staging", tt.origin)).To(Equal(tt.want))
		})
	}
}

type panicFS struct {
	filesys.FileSystem
}

func (panicFS) ReadFile(string) ([]byte, error) {
	panic("invalid object data")
}

func TestBuildRecoversPanic(t *testing.T) {
	g := NewWithT(t)
	fs := filesys.MakeFsInMemory()
	g.Expect(fs.WriteFile("/kustomization.yaml", []byte("resources: []"))).To(Succeed())
	res, err := build(panicFS{FileSystem: fs}, "/")
	g.Expect(err).To(MatchError("recovered from kustomize build panic: invalid object data"))
	g.Expect(res).To(BeNil())
	_, err = Build("testdata/simple")
	g.Expect(err).NotTo(HaveOccurred())
}

func TestBuildOpenAPIIsolation(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("openapi:\n  path: schema.json\n"), 0o644)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, "schema.json"), []byte(`{
  "definitions": {
    "example.Widget": {
      "type": "object",
      "x-kubernetes-group-version-kind": [{"group":"example.com","version":"v1","kind":"Widget"}]
    }
  }
}`), 0o644)).To(Succeed())
	_, err := Build(dir)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(openapi.SchemaForResourceType(yaml.TypeMeta{APIVersion: "example.com/v1", Kind: "Widget"})).To(BeNil())
	g.Expect(openapi.SchemaForResourceType(yaml.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"})).NotTo(BeNil())
}

func TestBuildConcurrent(t *testing.T) {
	for i := range 8 {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			docs, err := Build("testdata/overlay")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(docs).To(HaveLen(3))
		})
	}
}
