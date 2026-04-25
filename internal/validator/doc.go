// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package validator validates Kubernetes YAML manifests against JSON
// Schemas resolved from one or more schema location templates.
//
// # Entry points
//
// New returns a Validator configured from Options. ValidateSources walks
// files and directories and streams Results over a channel; ValidateBytes
// validates an in-memory payload and returns the Results slice.
//
// # YAML handling
//
// Multi-document streams are split on "\n---" boundaries. Documents that
// contain only comments or whitespace are dropped entirely rather than
// surfaced as skipped, keeping user-visible document numbering aligned
// with the real resources in each file.
//
// YAML is decoded in strict mode, so duplicate keys fail the document.
// When strict decoding fails, a lenient re-parse recovers apiVersion,
// kind, namespace, and name for the Result so callers can still render
// a meaningful identifier for the failing document.
//
// # Admission and schema checks
//
// For every decoded document the pipeline runs, in order:
//
//  1. SkipKinds matching — a pattern of "Kind" or "apiVersion/Kind"
//     short-circuits validation with StatusSkipped, before the admission
//     rule so encrypted or sealed manifests that omit metadata.name are
//     still skipped cleanly.
//  2. apiVersion/kind presence — missing either field fails the document;
//     Options.SkipMissingSchemas downgrades this to StatusSkipped.
//  3. Admission rule — metadata.name or metadata.generateName must be
//     set, matching kube-apiserver behavior.
//  4. Schema resolution — each location template is rendered with the
//     document's group/version/kind and the first matching schema is
//     compiled and cached. 404 / ENOENT on every location fails the
//     document unless Options.SkipMissingSchemas is set.
//  5. JSON Schema validation — the compiled schema is run against the
//     decoded document; per-field violations are returned as a flat list
//     of ValidationError with JSON Pointer paths.
//  6. ObjectMeta validation — DNS-1123 name/generateName/namespace and
//     qualified-name label/annotation key+value rules apply to every
//     doc, since CRDs and native Kubernetes schemas leave metadata
//     effectively unconstrained. Violations merge into the schema
//     error list under ReasonSchemaViolation.
//
// Schemas produced by the extractor package close objects with
// additionalProperties: false, so undocumented fields under spec fail
// validation.
//
// # Schema resolution and caching
//
// SchemaLoader renders each location template with the document's
// group/version/kind and loads from http(s) URLs (via retryablehttp,
// honoring Options.HTTPTimeout) or the local filesystem. Each rendered
// location is fetched, parsed, and compiled at most once per Validator
// lifetime, and the compiled *jsonschema.Schema is reused across
// documents. Compilation uses JSON Schema Draft 2020-12 with the
// Kubernetes string formats (duration, date, datetime/date-time, time)
// registered on the compiler — including duration units kube-apiserver
// accepts but Go's time.ParseDuration rejects (e.g. "2w", "3d").
//
// # Concurrency and streaming
//
// ValidateSources walks sources sequentially on one producer goroutine
// and validates documents in parallel on a pool of Options.Workers
// workers. Results arrive on the returned channel in completion order,
// which is non-deterministic; each Result carries Source and DocIndex
// so callers can reorder. After every real Result for a source has been
// pushed, a synthetic Result with Final=true is emitted for that source
// so consumers can flush per-source state mid-stream instead of
// buffering until end-of-stream. The channel is closed once all
// documents and sentinels have been delivered.
package validator
