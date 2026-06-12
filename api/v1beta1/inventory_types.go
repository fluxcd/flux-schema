// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// InventoryKind is the kind used by repository inventory envelopes.
	InventoryKind = "Inventory"

	// InventorySchema is the canonical URL of the JSON Schema describing the Inventory shape.
	InventorySchema = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/inventory/inventory-v1beta1.json"
)

// Inventory is the repository inventory envelope.
//
// +kubebuilder:object:root=true
type Inventory struct {
	// TypeMeta identifies the inventory API version and kind.
	metav1.TypeMeta `json:",inline"`

	// Schema identifies the JSON Schema for this inventory.
	// +optional
	Schema string `json:"$schema,omitempty"`

	// Inventory contains the repository inventory data.
	Inventory InventorySpec `json:"inventory"`
}

// InventorySpec contains repository inventory details.
type InventorySpec struct {
	// Reporter identifies the software that produced the inventory.
	// +kubebuilder:validation:Pattern=`^flux-schema/`
	Reporter string `json:"reporter"`

	// Timestamp is the inventory creation time in RFC 3339 format.
	// +kubebuilder:validation:Format=date-time
	Timestamp string `json:"timestamp"`

	// Summary contains aggregate resource counts.
	Summary InventorySummary `json:"summary"`

	// Directories maps every directory containing scanned manifests, plus
	// Helm chart and Terraform directories, to its classification. Paths
	// are relative to the scanned root and "/"-separated; the root itself
	// is ".".
	Directories map[string]InventoryDirectoryType `json:"directories"`

	// Resources maps "apiVersion/Kind" keys to the number of resources
	// of that type, covering Flux and plain Kubernetes resources alike.
	Resources map[string]int `json:"resources"`

	// Flux maps Flux custom resource kinds to the files defining them:
	// each kind holds a map of file path (relative to the scanned root)
	// to the "namespace/name" identities declared in that file. The
	// namespace segment is omitted for resources without a namespace and
	// the name is "-" when the document has no metadata.name. The
	// apiVersion of each kind is available in Resources.
	Flux map[string]map[string][]string `json:"flux"`
}

// InventorySummary contains aggregate scan counts.
type InventorySummary struct {
	// Files is the number of YAML files scanned.
	// +kubebuilder:validation:Minimum=0
	Files int `json:"files"`

	// Resources is the total number of resources found.
	// +kubebuilder:validation:Minimum=0
	Resources int `json:"resources"`

	// LinesOfYAML is the total number of lines in the scanned YAML files.
	// +kubebuilder:validation:Minimum=0
	LinesOfYAML int `json:"lines-of-yaml"`
}

// InventoryDirectoryType classifies a directory in the repository.
//
// +kubebuilder:validation:Enum=kubernetes-manifests;kustomize-overlay;helm-chart;terraform-module
// +k8s:enum
type InventoryDirectoryType string

const (
	// InventoryDirectoryKubernetesManifests means the directory contains
	// plain Kubernetes YAML manifests.
	InventoryDirectoryKubernetesManifests InventoryDirectoryType = "kubernetes-manifests"

	// InventoryDirectoryKustomizeOverlay means the directory contains a
	// kustomize configuration file.
	InventoryDirectoryKustomizeOverlay InventoryDirectoryType = "kustomize-overlay"

	// InventoryDirectoryHelmChart means the directory contains a Helm
	// chart; its contents are not scanned.
	InventoryDirectoryHelmChart InventoryDirectoryType = "helm-chart"

	// InventoryDirectoryTerraformModule means the directory contains a
	// Terraform module; its contents are not scanned.
	InventoryDirectoryTerraformModule InventoryDirectoryType = "terraform-module"
)
