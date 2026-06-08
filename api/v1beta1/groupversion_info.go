// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package v1beta1 contains the API Schema definitions for flux-schema.
// +kubebuilder:object:generate=true
// +groupName=schema.plugin.fluxcd.io
package v1beta1

import "k8s.io/apimachinery/pkg/runtime/schema"

// GroupVersion identifies the schema API group and version.
var GroupVersion = schema.GroupVersion{Group: "schema.plugin.fluxcd.io", Version: "v1beta1"}
