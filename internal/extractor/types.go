// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

// Schema is a single Kubernetes kind paired with its transformed JSON Schema.
// JSON is owned by the instance; the transforms mutate it in place, so callers
// should treat it as single-use.
type Schema struct {
	// Group is the Kubernetes API group (e.g. "source.toolkit.fluxcd.io").
	// Empty for core/v1 kinds; callers may normalize that to "core".
	Group string

	// Version is the API version of the kind (e.g. "v1", "v1beta2").
	Version string

	// Kind is the Kubernetes kind name (e.g. "GitRepository", "Pod").
	Kind string

	// JSON is the transformed JSON Schema document for this kind, decoded
	// with json.Decoder.UseNumber so numeric literals round-trip exactly.
	JSON map[string]any
}

// GVK identifies a Kubernetes kind by its API group, version, and kind name.
// It mirrors the entries found under x-kubernetes-group-version-kind in
// OpenAPI swagger definitions.
type GVK struct {
	// Group is the Kubernetes API group, empty for the core group.
	Group string

	// Version is the API version (e.g. "v1", "v1beta2").
	Version string

	// Kind is the Kubernetes kind name (e.g. "Pod", "Deployment").
	Kind string
}
