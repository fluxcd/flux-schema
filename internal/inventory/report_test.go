// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"encoding/json"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

func TestNewInventory(t *testing.T) {
	g := NewWithT(t)

	res := &Result{
		Resources: []Resource{
			{APIVersion: "kustomize.toolkit.fluxcd.io/v1", Kind: "Kustomization",
				Name: "apps", Namespace: "flux-system", Source: "clusters/prod/apps.yaml"},
			{APIVersion: "kustomize.toolkit.fluxcd.io/v1", Kind: "Kustomization",
				Name: "infra", Namespace: "flux-system", Source: "clusters/prod/infra.yaml"},
			{APIVersion: "helm.toolkit.fluxcd.io/v2", Kind: "HelmRelease",
				Name: "podinfo", Namespace: "apps", Source: "apps/base/podinfo.yaml"},
			{APIVersion: "apps/v1", Kind: "Deployment",
				Name: "app", Namespace: "default", Source: "apps/base/deploy.yaml"},
			{APIVersion: "v1", Kind: "Service",
				Name: "app", Namespace: "default", Source: "apps/base/deploy.yaml"},
			{APIVersion: "v1", Kind: "Namespace",
				Name: "apps", Source: "apps/base/ns.yaml"},
		},
		DirTypes: map[string]apiv1.InventoryDirectoryType{
			"apps/base":      apiv1.InventoryDirectoryKustomizeOverlay,
			"charts/podinfo": apiv1.InventoryDirectoryHelmChart,
			"infra/tf":       apiv1.InventoryDirectoryTerraformModule,
		},
		Files: 5,
		Lines: 120,
	}

	ts := time.Date(2026, 6, 11, 10, 30, 0, 0, time.UTC)
	inv := NewInventory("flux-schema/0.0.0-test", ts, res)

	g.Expect(inv.APIVersion).To(Equal("schema.plugin.fluxcd.io/v1beta1"))
	g.Expect(inv.Kind).To(Equal(apiv1.InventoryKind))
	g.Expect(inv.Schema).To(Equal(apiv1.InventorySchema))
	g.Expect(inv.Inventory.Reporter).To(Equal("flux-schema/0.0.0-test"))
	g.Expect(inv.Inventory.Timestamp).To(Equal("2026-06-11T10:30:00Z"))

	g.Expect(inv.Inventory.Summary).To(Equal(apiv1.InventorySummary{
		Files: 5, Resources: 6, LinesOfYAML: 120,
	}))

	// The census covers Flux and Kubernetes resources alike.
	g.Expect(inv.Inventory.Resources).To(Equal(map[string]int{
		"kustomize.toolkit.fluxcd.io/v1/Kustomization": 2,
		"helm.toolkit.fluxcd.io/v2/HelmRelease":        1,
		"apps/v1/Deployment":                           1,
		"v1/Service":                                   1,
		"v1/Namespace":                                 1,
	}))

	// Flux kinds map each defining file to the identities it declares.
	g.Expect(inv.Inventory.Flux).To(Equal(map[string]map[string][]string{
		"HelmRelease": {
			"apps/base/podinfo.yaml": {"apps/podinfo"},
		},
		"Kustomization": {
			"clusters/prod/apps.yaml":  {"flux-system/apps"},
			"clusters/prod/infra.yaml": {"flux-system/infra"},
		},
	}))

	// Untyped dirs default to kubernetes-manifests; typed dirs keep
	// their classification even without countable resources.
	g.Expect(inv.Inventory.Directories).To(Equal(map[string]apiv1.InventoryDirectoryType{
		"apps/base":      apiv1.InventoryDirectoryKustomizeOverlay,
		"charts/podinfo": apiv1.InventoryDirectoryHelmChart,
		"clusters/prod":  apiv1.InventoryDirectoryKubernetesManifests,
		"infra/tf":       apiv1.InventoryDirectoryTerraformModule,
	}))
}

func TestNewInventoryIdentity(t *testing.T) {
	g := NewWithT(t)

	res := &Result{
		Resources: []Resource{
			{APIVersion: "source.toolkit.fluxcd.io/v1", Kind: "GitRepository",
				Source: "unnamed.yaml"},
			{APIVersion: "kustomize.toolkit.fluxcd.io/v1", Kind: "Kustomization",
				Name: "apps", Namespace: "flux-system", Source: "multi.yaml"},
			{APIVersion: "kustomize.toolkit.fluxcd.io/v1", Kind: "Kustomization",
				Name: "infra", Namespace: "flux-system", Source: "multi.yaml"},
		},
	}
	inv := NewInventory("flux-schema/0.0.0-test", time.Unix(0, 0), res)

	// Unnamed documents fall back to the "-" placeholder; multi-doc
	// files list every identity in document order.
	g.Expect(inv.Inventory.Flux).To(Equal(map[string]map[string][]string{
		"GitRepository": {
			"unnamed.yaml": {"-"},
		},
		"Kustomization": {
			"multi.yaml": {"flux-system/apps", "flux-system/infra"},
		},
	}))
}

func TestNewInventoryEmpty(t *testing.T) {
	g := NewWithT(t)

	inv := NewInventory("flux-schema/0.0.0-test", time.Unix(0, 0), &Result{})

	data, err := json.Marshal(inv)
	g.Expect(err).ToNot(HaveOccurred())

	// Empty collections serialize as {}, never null.
	g.Expect(string(data)).To(ContainSubstring(`"directories":{}`))
	g.Expect(string(data)).To(ContainSubstring(`"resources":{}`))
	g.Expect(string(data)).To(ContainSubstring(`"flux":{}`))
	g.Expect(string(data)).ToNot(ContainSubstring("null"))
}
