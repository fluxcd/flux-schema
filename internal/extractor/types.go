// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"slices"
	"strings"
)

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

	// Scope is the Kubernetes resource scope: "Namespaced", "Cluster", or empty
	// when unknown.
	Scope string

	// Source is the upstream source system and version the schema was extracted
	// from. Empty when unknown.
	Source string

	// Deprecated reports whether this API version is deprecated.
	Deprecated bool

	// DeprecationWarning is the optional warning message for a deprecated API
	// version.
	DeprecationWarning string

	// Resource contains discovery names used to resolve kubectl-style resource
	// references, such as plurals and short names.
	Resource ResourceNames

	// JSON is the transformed JSON Schema document for this kind, decoded
	// with json.Decoder.UseNumber so numeric literals round-trip exactly.
	JSON map[string]any
}

// ResourceNames contains Kubernetes discovery names for a resource.
type ResourceNames struct {
	// Singular is the resource singular name reported by discovery.
	Singular string

	// Plural is the resource plural name reported by discovery.
	Plural string

	// ShortNames are the resource short names reported by discovery.
	ShortNames []string
}

// ExplainAliases returns normalized resource aliases that should resolve to the
// schema's canonical kind for kubectl-compatible explain resource references.
func (s Schema) ExplainAliases() []string {
	var out []string
	add := func(v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || v == strings.ToLower(s.Kind) || slices.Contains(out, v) {
			return
		}
		out = append(out, v)
	}
	add(s.Resource.Singular)
	add(s.Resource.Plural)
	for _, name := range s.Resource.ShortNames {
		add(name)
	}
	return out
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
