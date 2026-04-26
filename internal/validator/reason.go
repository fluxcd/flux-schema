// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import "strings"

// Reason is a stable, machine-readable code describing why a Result has
// its Status. It is treated as API surface: adding a new Reason is
// backwards-compatible, but renaming or repurposing an existing value is
// not. The kebab-case wire form doubles as the human-readable form for
// text output via Reason.String (dashes become spaces), so there is only
// one string to maintain per case.
type Reason string

const (
	// ReasonNone is the zero value, carried by every StatusValid result.
	ReasonNone Reason = ""

	// ReasonSourceLoadError indicates the input source itself could not be
	// read (file open/stat failure, stdin read error, etc.). The raw error
	// text lands in Errors[0].Msg.
	ReasonSourceLoadError Reason = "source-load-error"

	// ReasonYAMLParseError indicates strict YAML decoding failed — malformed
	// document, duplicate keys, or other structural issues. Per-violation
	// detail is in Errors.
	ReasonYAMLParseError Reason = "yaml-parse-error"

	// ReasonKindSkipped indicates the document was skipped because its
	// apiVersion/Kind matched a --skip-kind pattern.
	ReasonKindSkipped Reason = "kind-skipped"

	// ReasonSchemaLoadError indicates the schema loader failed (HTTP fetch,
	// file read, or JSON Schema compile). Raw error text is in Errors[0].Msg.
	ReasonSchemaLoadError Reason = "schema-load-error"

	// ReasonSchemaNotFound indicates no schema could be applied to the
	// document — either no schema file matched the GVK, or the document has
	// no GVK to look up (under --skip-missing-schemas).
	ReasonSchemaNotFound Reason = "schema-not-found"

	// ReasonSchemaViolation indicates the document failed one or more schema
	// constraints. Errors carries one entry per violation with a JSON Pointer
	// Path to the offending field.
	ReasonSchemaViolation Reason = "schema-violation"

	// ReasonCELViolation indicates the document failed one or more CEL rules
	// declared via x-kubernetes-validations in the schema, or the schema's
	// CEL evaluator could not be built. Distinct from ReasonSchemaViolation
	// so report consumers can filter on it; the underlying JSON Schema
	// constraints all passed.
	ReasonCELViolation Reason = "cel-violation"
)

// String returns the human-readable form used in text output: the kebab-case
// value with dashes replaced by spaces (e.g. "schema-not-found" → "schema
// not found"). An empty Reason returns an empty string, which the CLI
// renderer treats as "no reason suffix."
func (r Reason) String() string {
	return strings.ReplaceAll(string(r), "-", " ")
}
