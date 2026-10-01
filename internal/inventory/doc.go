// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package inventory catalogs the Kubernetes manifests in a GitOps
// repository for consumption by AI agents and other automation.
//
// # Entry points
//
// Scan walks a directory (or reads a single file) and returns a Result
// listing every Kubernetes resource with its defining file, the
// classification of every directory, and the number of YAML files read.
// NewInventory converts a Result into the versioned Inventory envelope.
//
// # Classification
//
// The first document of a recognized kustomization file
// (kustomization.yaml, kustomization.yml, Kustomization), with or without
// apiVersion and kind, and documents in the kustomize.config.k8s.io group
// mark their directory as a kustomize overlay, or as a kustomize component
// for kind Component, and are not counted as resources. Later documents of
// a kustomization file are ignored, as kustomize only reads the first.
// Overlays reachable through resources, components or bases from the
// kustomization file of an unreferenced overlay are reclassified as
// kustomize bases. Remote and out-of-root references are ignored.
//
// Any other YAML document needs both apiVersion and kind, otherwise it is
// ignored. Documents whose API group contains "fluxcd" are Flux resources;
// everything else is a plain Kubernetes resource.
//
// Directories containing a Chart.yaml are classified as Helm charts and
// directories containing Terraform files as Terraform modules; both are
// pruned from the walk so nothing below them is scanned. When a directory
// holds both, the Helm chart classification wins.
//
// Files referenced as kustomize patches (patches, patchesStrategicMerge,
// patchesJson6902) are excluded from the inventory: their documents look
// like full manifests but only describe modifications to resources
// defined elsewhere, so counting them would duplicate resources.
package inventory
