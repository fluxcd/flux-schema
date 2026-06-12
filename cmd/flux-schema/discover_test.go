// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

const discoverFixture = "testdata/discover/repo"

func TestDiscoverCmd(t *testing.T) {
	g := NewWithT(t)

	output, err := executeCommand([]string{"discover", discoverFixture})
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(output).To(ContainSubstring("Resources:"))
	g.Expect(output).To(ContainSubstring("  helm.toolkit.fluxcd.io/v2/HelmRelease: 1"))
	g.Expect(output).To(ContainSubstring("  kustomize.toolkit.fluxcd.io/v1/Kustomization: 1"))
	g.Expect(output).To(ContainSubstring("  v1/ConfigMap: 1"))
	g.Expect(output).To(ContainSubstring("  apps/v1/Deployment: 1"))
	g.Expect(output).To(ContainSubstring("Flux:"))
	g.Expect(output).To(ContainSubstring("  HelmRelease:\n    apps/base/podinfo.yaml: podinfo/podinfo"))
	g.Expect(output).To(ContainSubstring("  Kustomization:\n    clusters/prod/flux-system.yaml: flux-system/apps"))
	g.Expect(output).To(ContainSubstring("Directories:"))
	g.Expect(output).To(ContainSubstring("  apps/base: kustomize-overlay"))
	g.Expect(output).To(ContainSubstring("  charts/podinfo: helm-chart"))
	g.Expect(output).To(ContainSubstring("  infra/tf: terraform-module"))
	g.Expect(output).To(ContainSubstring("  legacy: kubernetes-manifests"))
	g.Expect(output).To(ContainSubstring(
		"Summary: 7 resources found in 8 files with 92 lines of YAML"))
}

func TestDiscoverCmdJSON(t *testing.T) {
	g := NewWithT(t)

	output, err := executeCommand([]string{"discover", discoverFixture, "-o", "json"})
	g.Expect(err).ToNot(HaveOccurred())

	var inv apiv1.Inventory
	g.Expect(json.Unmarshal([]byte(output), &inv)).To(Succeed())

	g.Expect(inv.APIVersion).To(Equal("schema.plugin.fluxcd.io/v1beta1"))
	g.Expect(inv.Kind).To(Equal(apiv1.InventoryKind))
	g.Expect(inv.Schema).To(Equal(apiv1.InventorySchema))
	g.Expect(inv.Inventory.Reporter).To(HavePrefix("flux-schema/"))

	g.Expect(inv.Inventory.Summary).To(Equal(apiv1.InventorySummary{
		Files: 8, Resources: 7, LinesOfYAML: 92,
	}))

	g.Expect(inv.Inventory.Resources).To(Equal(map[string]int{
		"fluxcd.controlplane.io/v1/FluxInstance":       1,
		"helm.toolkit.fluxcd.io/v2/HelmRelease":        1,
		"kustomize.toolkit.fluxcd.io/v1/Kustomization": 1,
		"source.toolkit.fluxcd.io/v1/GitRepository":    1,
		"v1/ConfigMap":       1,
		"apps/v1/Deployment": 1,
		"v1/Service":         1,
	}))

	g.Expect(inv.Inventory.Flux).To(Equal(map[string]map[string][]string{
		"FluxInstance": {
			"clusters/prod/instance.yaml": {"flux-system/flux"},
		},
		"HelmRelease": {
			"apps/base/podinfo.yaml": {"podinfo/podinfo"},
		},
		"Kustomization": {
			"clusters/prod/flux-system.yaml": {"flux-system/apps"},
		},
		"GitRepository": {
			"clusters/prod/flux-system.yaml": {"flux-system/flux-system"},
		},
	}))

	g.Expect(inv.Inventory.Directories).To(Equal(map[string]apiv1.InventoryDirectoryType{
		"apps/base":          apiv1.InventoryDirectoryKustomizeOverlay,
		"apps/overlays/prod": apiv1.InventoryDirectoryKustomizeOverlay,
		"charts/podinfo":     apiv1.InventoryDirectoryHelmChart,
		"clusters/prod":      apiv1.InventoryDirectoryKubernetesManifests,
		"infra/tf":           apiv1.InventoryDirectoryTerraformModule,
		"legacy":             apiv1.InventoryDirectoryKubernetesManifests,
	}))
}

func TestDiscoverCmdYAML(t *testing.T) {
	g := NewWithT(t)

	output, err := executeCommand([]string{"discover", discoverFixture, "-o", "yaml"})
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(output).ToNot(ContainSubstring("$schema"))

	var inv apiv1.Inventory
	g.Expect(yaml.Unmarshal([]byte(output), &inv)).To(Succeed())
	g.Expect(inv.Kind).To(Equal(apiv1.InventoryKind))
	g.Expect(inv.Inventory.Summary.Resources).To(Equal(7))
}

func TestDiscoverCmdSkipFile(t *testing.T) {
	g := NewWithT(t)

	output, err := executeCommand([]string{
		"discover", discoverFixture, "--skip-file", ".*", "--skip-file", "legacy"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(output).To(ContainSubstring(
		"Summary: 6 resources found in 7 files with 87 lines of YAML"))
}

func TestDiscoverCmdDefaultPath(t *testing.T) {
	g := NewWithT(t)
	t.Chdir(discoverFixture)

	output, err := executeCommand([]string{"discover"})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(output).To(ContainSubstring(
		"Summary: 7 resources found in 8 files with 92 lines of YAML"))
}

func TestDiscoverCmdErrors(t *testing.T) {
	g := NewWithT(t)

	_, err := executeCommand([]string{"discover", "-"})
	g.Expect(err).To(MatchError(ContainSubstring("does not read from stdin")))

	_, err = executeCommand([]string{"discover", discoverFixture, "-o", "junit"})
	g.Expect(err).To(MatchError(ContainSubstring("unsupported output format")))

	_, err = executeCommand([]string{"discover", "testdata/discover/missing"})
	g.Expect(err).To(HaveOccurred())
}
