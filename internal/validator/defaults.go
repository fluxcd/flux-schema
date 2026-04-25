// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

// DefaultSchemaLocation points at the flux-schema catalog, covering the
// latest stable Kubernetes and Flux APIs. It is used when no schema location
// is configured, and is the target that the literal value "default" resolves
// to in the validate CLI's --schema-location flag.
const DefaultSchemaLocation = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/catalog/latest/" + DefaultSchemaLayout

// DefaultSchemaLayout is the Go-template tail appended to any schema location
// value that doesn't already end in ".json", so a bare path/URL like
// "./my-schemas" expands to "./my-schemas/{{.Group}}/{{.Kind}}_{{.Version}}.json".
const DefaultSchemaLayout = "{{.Group}}/{{.Kind}}_{{.Version}}.json"

const DefaultWorkers = 8

// StdinSource is the canonical source label for documents read from an
// io.Reader rather than a file path. Options.Stdin must be non-nil when any
// input path equals this sentinel; callers typically pass os.Stdin.
const StdinSource = "stdin"
