// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package extractor converts Kubernetes API definitions into standalone-strict
// JSON Schema documents suitable for offline validators and editor tooling.
//
// Three entry points are provided:
//
//   - ExtractCRDs reads a YAML payload containing CustomResourceDefinitions
//     (bare, List-wrapped, or multi-document — e.g. `kubectl get crds -o yaml`)
//     and returns one Schema per CRD version, taken from
//     spec.versions[].schema.openAPIV3Schema.
//
//   - ExtractKubernetes reads a Kubernetes OpenAPI v2 swagger document (e.g.
//     `kubectl get --raw /openapi/v2`) and returns one Schema per
//     x-kubernetes-group-version-kind entry, with all $refs inlined.
//
//   - ExtractOpenShift reads an openshift/api OpenAPI v2 swagger document
//     and returns one Schema per top-level OpenShift resource. The OpenShift
//     swagger carries no x-kubernetes-group-version-kind annotations, so
//     the GVK is derived from the definition key
//     (com.github.openshift.api.<dir>.<version>.<Kind>) and the dir is
//     mapped to its canonical group via a hand-curated table. Only
//     definitions in the com.github.openshift.api.* namespace are emitted;
//     the upstream Kubernetes types embedded in the same document are
//     inlined when referenced but never written as standalone Schemas.
//
// All three entry points return []Schema, where each Schema carries the GVK
// coordinates and the transformed JSON document. The Schema's JSON map is
// owned by the instance: transforms mutate it in place, so callers should
// treat each Schema as single-use.
//
// Errors are aggregated rather than fatal: a malformed document or
// definition does not stop extraction of the rest. Numeric literals are
// preserved exactly via json.Decoder.UseNumber so generated schemas
// round-trip without float coercion.
//
// The transform pipeline (in transform.go and the *Refs / *Nullable /
// *VendorExtensions helpers in openapi.go) applies, in order: $ref inlining,
// apiVersion/kind injection, int-or-string rewriting, nullable-optional
// marking, additionalProperties:false closure, and vendor-extension
// stripping (preserving x-kubernetes-preserve-unknown-fields).
package extractor
