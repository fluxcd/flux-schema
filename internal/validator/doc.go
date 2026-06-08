// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package validator validates Kubernetes YAML manifests against JSON
// Schemas resolved from one or more schema location templates.
//
// # Entry points
//
// New returns a Validator from Options. ValidateSources walks files and
// directories and streams Results over a channel; ValidateBytes validates
// an in-memory payload and returns a Results slice.
//
// # YAML handling
//
// Multi-document streams are split on "\n---" boundaries. Documents
// containing only comments or whitespace are dropped before doc numbering
// so user-visible indices align with real resources. YAML is decoded in
// strict mode (duplicate keys fail); a lenient re-parse on failure
// recovers apiVersion/kind/namespace/name for the Result identifier.
//
// # Per-document pipeline
//
// For every decoded document, in order:
//
//  1. SkipKinds — "kind" or "apiVersion/kind" patterns short-circuit to
//     StatusSkipped before admission and schema checks.
//  2. apiVersion/kind presence — missing either fails the document
//     unless SkipMissingSchemas is set.
//  3. Admission — metadata.name or metadata.generateName must be set, except
//     for Flux plugin API groups.
//  4. Schema resolution — each location template is rendered with the
//     document's GVK; the first hit is compiled and cached. 404 / ENOENT
//     on every location fails unless SkipMissingSchemas is set.
//  5. SkipJSONPaths — matching pointers are deleted from the document
//     before schema validation.
//  6. JSON Schema + ObjectMeta — the compiled schema runs against the
//     document; DNS-1123 and qualified-name rules are layered on metadata
//     since Kubernetes schemas leave it effectively unconstrained. The
//     metadata layer is skipped for Flux plugin API groups. Violations merge
//     under ReasonSchemaViolation.
//  7. CEL x-kubernetes-validations — runs only after steps 1-6 pass and
//     unless Options.SkipCELRules is set. Rule compile errors and runtime
//     violations both surface as ReasonCELViolation; oldSelf is unbound
//     (static validator, no transition state).
//
// Schemas produced by the extractor close objects with
// additionalProperties: false, so undocumented spec fields fail.
//
// # Schema resolution and caching
//
// SchemaLoader renders each location template with the document's GVK
// and loads from http(s) (via retryablehttp, honoring HTTPTimeout) or
// the local filesystem. Each rendered location is fetched, parsed, and
// compiled at most once per Validator lifetime. Compilation uses Draft
// 2020-12 with Kubernetes string formats registered (duration, date,
// datetime/date-time, time) — including duration units kube-apiserver
// accepts but Go's time.ParseDuration rejects (e.g. "2w", "3d").
//
// # Concurrency and streaming
//
// ValidateSources walks sources on one producer goroutine and validates
// documents on a pool of Options.Workers workers. Results arrive in
// completion order (non-deterministic); each Result carries Source and
// DocIndex for reordering. After the last real Result for a source, a
// synthetic Result with Final=true is emitted so consumers can flush
// per-source state mid-stream. The channel closes once all documents
// and sentinels have been delivered.
package validator
