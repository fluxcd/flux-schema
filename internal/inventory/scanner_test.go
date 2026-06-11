// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

// writeTree materializes files (slash-separated path → content) under a
// fresh temporary directory and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return root
}

const deploymentYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
`

func TestScanClassification(t *testing.T) {
	g := NewWithT(t)
	root := writeTree(t, map[string]string{
		"clusters/prod/sync.yaml": `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: flux-system
  namespace: flux-system
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: apps
  namespace: flux-system
`,
		"clusters/prod/instance.yaml": `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
`,
		"apps/base/deployment.yaml": deploymentYAML + `---
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
`,
		"apps/base/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`,
		"apps/base/notes.yaml": `# only comments
---
config: not-a-kubernetes-doc
`,
		"legacy/app.yml": deploymentYAML,
	})

	res, err := Scan(root, Options{})
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(res.Files).To(Equal(6))
	g.Expect(res.Lines).To(Equal(39))
	g.Expect(res.DirTypes).To(Equal(map[string]apiv1.InventoryDirectoryType{
		"apps/base": apiv1.InventoryDirectoryKustomizeOverlay,
	}))

	sources := make([]string, 0, len(res.Resources))
	var flux, k8s int
	for _, r := range res.Resources {
		sources = append(sources, r.Source)
		if IsFluxResource(r.APIVersion) {
			flux++
		} else {
			k8s++
		}
	}
	g.Expect(sources).To(ConsistOf(
		"clusters/prod/sync.yaml", "clusters/prod/sync.yaml",
		"clusters/prod/instance.yaml",
		"apps/base/deployment.yaml", "apps/base/deployment.yaml",
		"legacy/app.yml",
	))
	g.Expect(flux).To(Equal(3))
	g.Expect(k8s).To(Equal(3))
}

func TestScanExcludedDirs(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		opts          Options
		wantDirTypes  map[string]apiv1.InventoryDirectoryType
		wantResources int
		wantFiles     int
	}{
		{
			name: "helm chart pruned",
			files: map[string]string{
				"charts/podinfo/Chart.yaml":            "name: podinfo\n",
				"charts/podinfo/templates/deploy.yaml": deploymentYAML,
				"apps/deploy.yaml":                     deploymentYAML,
			},
			wantDirTypes: map[string]apiv1.InventoryDirectoryType{
				"charts/podinfo": apiv1.InventoryDirectoryHelmChart,
			},
			wantResources: 1,
			wantFiles:     1,
		},
		{
			name: "terraform pruned",
			files: map[string]string{
				"infra/tf/main.tf":        "resource {}\n",
				"infra/tf/configmap.yaml": deploymentYAML,
				"apps/deploy.yaml":        deploymentYAML,
			},
			wantDirTypes: map[string]apiv1.InventoryDirectoryType{
				"infra/tf": apiv1.InventoryDirectoryTerraformModule,
			},
			wantResources: 1,
			wantFiles:     1,
		},
		{
			name: "chart wins over terraform",
			files: map[string]string{
				"mixed/Chart.yaml": "name: mixed\n",
				"mixed/main.tf":    "resource {}\n",
			},
			wantDirTypes: map[string]apiv1.InventoryDirectoryType{
				"mixed": apiv1.InventoryDirectoryHelmChart,
			},
		},
		{
			name: "dot dirs skipped by default",
			files: map[string]string{
				".github/workflows/ci.yaml": deploymentYAML,
				"apps/deploy.yaml":          deploymentYAML,
			},
			wantDirTypes:  map[string]apiv1.InventoryDirectoryType{},
			wantResources: 1,
			wantFiles:     1,
		},
		{
			name: "custom skip pattern",
			files: map[string]string{
				"tests/deploy.yaml": deploymentYAML,
				"apps/deploy.yaml":  deploymentYAML,
			},
			opts:          Options{SkipFiles: []string{".*", "tests"}},
			wantDirTypes:  map[string]apiv1.InventoryDirectoryType{},
			wantResources: 1,
			wantFiles:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			root := writeTree(t, tt.files)
			res, err := Scan(root, tt.opts)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(res.DirTypes).To(Equal(tt.wantDirTypes))
			g.Expect(res.Resources).To(HaveLen(tt.wantResources))
			g.Expect(res.Files).To(Equal(tt.wantFiles))
		})
	}
}

func TestScanPatchExclusion(t *testing.T) {
	g := NewWithT(t)
	root := writeTree(t, map[string]string{
		"apps/base/deployment.yaml": deploymentYAML,
		"apps/base/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
`,
		// patches[].path, a path-form patchesStrategicMerge entry, an
		// inline strategic-merge patch, and a ../ reference into a
		// sibling directory.
		"apps/overlays/prod/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../base
patches:
  - path: patch-deploy.yaml
patchesStrategicMerge:
  - legacy-patch.yaml
  - |-
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: app
patchesJson6902:
  - path: ../shared/patch-ops.yaml
`,
		"apps/overlays/prod/patch-deploy.yaml": deploymentYAML,
		"apps/overlays/prod/legacy-patch.yaml": deploymentYAML,
		"apps/overlays/shared/patch-ops.yaml":  deploymentYAML,
		"apps/overlays/prod/extra.yaml":        deploymentYAML,
	})

	res, err := Scan(root, Options{})
	g.Expect(err).ToNot(HaveOccurred())

	sources := make([]string, 0, len(res.Resources))
	for _, r := range res.Resources {
		sources = append(sources, r.Source)
	}
	g.Expect(sources).To(ConsistOf(
		"apps/base/deployment.yaml",
		"apps/overlays/prod/extra.yaml",
	))
	// base deployment + extra.yaml; the three patch files are excluded
	// and kustomization.yaml files carry no countable documents.
	g.Expect(res.Files).To(Equal(4))
	// Excluded patch files contribute no lines either.
	g.Expect(res.Lines).To(Equal(29))
	g.Expect(res.DirTypes).To(HaveKeyWithValue(
		"apps/overlays/prod", apiv1.InventoryDirectoryKustomizeOverlay))
}

func TestScanIgnoresSymlinks(t *testing.T) {
	g := NewWithT(t)
	base := t.TempDir()
	root := writeTree(t, map[string]string{
		"apps/deploy.yaml": deploymentYAML,
	})
	if err := os.WriteFile(filepath.Join(base, "secret.yaml"), []byte(`apiVersion: v1
kind: Secret
metadata:
  name: outside
`), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	// A symlinked file and directory escaping the root, plus a
	// symlinked file inside the root.
	for link, target := range map[string]string{
		"apps/escape.yaml": filepath.Join(base, "secret.yaml"),
		"linkdir":          base,
		"apps/local.yaml":  "deploy.yaml",
	} {
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
	}

	res, err := Scan(root, Options{})
	g.Expect(err).ToNot(HaveOccurred())

	// Only the real file is scanned: symlinks are never followed,
	// whether they escape the root or not.
	g.Expect(res.Files).To(Equal(1))
	g.Expect(res.Resources).To(HaveLen(1))
	g.Expect(res.Resources[0].Source).To(Equal("apps/deploy.yaml"))
}

func TestScanSingleFile(t *testing.T) {
	g := NewWithT(t)
	root := writeTree(t, map[string]string{
		"manifests/deploy.yaml": deploymentYAML,
	})

	res, err := Scan(filepath.Join(root, "manifests", "deploy.yaml"), Options{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.Files).To(Equal(1))
	g.Expect(res.Resources).To(HaveLen(1))
	g.Expect(res.Resources[0].Source).To(Equal("deploy.yaml"))
}

func TestScanRootResources(t *testing.T) {
	g := NewWithT(t)
	root := writeTree(t, map[string]string{
		"deploy.yaml": deploymentYAML,
	})

	res, err := Scan(root, Options{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.Resources).To(HaveLen(1))
	g.Expect(res.Resources[0].Source).To(Equal("deploy.yaml"))
}

func TestScanErrors(t *testing.T) {
	g := NewWithT(t)

	_, err := Scan(filepath.Join(t.TempDir(), "missing"), Options{})
	g.Expect(err).To(HaveOccurred())

	_, err = Scan(t.TempDir(), Options{SkipFiles: []string{" "}})
	g.Expect(err).To(MatchError(ContainSubstring("must not be empty")))

	_, err = Scan(t.TempDir(), Options{SkipFiles: []string{"[invalid"}})
	g.Expect(err).To(MatchError(ContainSubstring("skip file pattern")))
}

func TestScanEmptyDir(t *testing.T) {
	g := NewWithT(t)

	res, err := Scan(t.TempDir(), Options{})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.Files).To(BeZero())
	g.Expect(res.Resources).To(BeEmpty())
	g.Expect(res.DirTypes).To(BeEmpty())
}

func TestIsFluxResource(t *testing.T) {
	g := NewWithT(t)

	g.Expect(IsFluxResource("source.toolkit.fluxcd.io/v1")).To(BeTrue())
	g.Expect(IsFluxResource("fluxcd.controlplane.io/v1")).To(BeTrue())
	g.Expect(IsFluxResource("apps/v1")).To(BeFalse())
	g.Expect(IsFluxResource("v1")).To(BeFalse())
}
