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

// DefaultWorkers is the fallback concurrency level used
// by ValidateSources when Options.Workers is left at 0.
const DefaultWorkers = 8
