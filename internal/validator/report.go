// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import "time"

// ReportVersion pins the wire format version emitted by NewReport.
const ReportVersion = "1.0.0"

// ReportSchema is the canonical URL of the JSON Schema describing the Report shape.
const ReportSchema = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/report/schema-1.0.0.json"

// Report is the envelope emitted when `flux-schema validate` runs with
// --output json or --output yaml. The JSON tags are authoritative; the YAML
// encoder (sigs.k8s.io/yaml) reflects them through JSON.
type Report struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema,omitempty"`
	Report  ReportBody `json:"report"`
}

type ReportBody struct {
	Reporter  string         `json:"reporter"`
	Timestamp string         `json:"timestamp"`
	Summary   ReportSummary  `json:"summary"`
	Results   []ReportResult `json:"results"`
}

type ReportSummary struct {
	Total   int `json:"total"`
	Valid   int `json:"valid"`
	Invalid int `json:"invalid"`
	Skipped int `json:"skipped"`
}

// ReportResult is the wire form of a single validated document. Resource is
// a pointer so the zero value renders as `"resource": null` rather than an
// empty object — consumers must tolerate a null resource when no identity
// could be recovered (file open failure, unparsable YAML, etc.).
type ReportResult struct {
	Resource   *ReportResource   `json:"resource"`
	Source     string            `json:"source"`
	Idx        int               `json:"idx"`
	Status     string            `json:"status"`
	Reason     Reason            `json:"reason,omitempty"`
	Violations []ReportViolation `json:"violations,omitempty"`
}

type ReportResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ReportViolation carries a JSON Pointer path only for ReasonSchemaViolation;
// for every other reason Path is empty and Message holds the raw error text.
type ReportViolation struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// NewReportResult converts a Result into its wire-format counterpart.
// Resource is nil when no apiVersion/kind could be recovered (source-load
// or unparsable-YAML failures); the pointer renders as `"resource": null`.
func NewReportResult(r Result) ReportResult {
	out := ReportResult{
		Source: r.Source,
		Idx:    r.DocIndex,
		Status: r.Status.String(),
		Reason: r.Reason,
	}
	if r.APIVersion != "" || r.Kind != "" {
		out.Resource = &ReportResource{
			APIVersion: r.APIVersion,
			Kind:       r.Kind,
			Namespace:  r.Namespace,
			Name:       r.Name,
		}
	}
	if len(r.Errors) > 0 {
		out.Violations = make([]ReportViolation, len(r.Errors))
		for i, e := range r.Errors {
			out.Violations[i] = ReportViolation{Path: e.Path, Message: e.Msg}
		}
	}
	return out
}

// NewReport assembles a Report from the validator outputs. The caller owns
// the reporter string (typically "flux-schema/"+VERSION) and the timestamp
// so tests can pin both.
func NewReport(reporter string, timestamp time.Time, results []Result, summary ReportSummary) Report {
	body := ReportBody{
		Reporter:  reporter,
		Timestamp: timestamp.UTC().Format(time.RFC3339),
		Summary:   summary,
		Results:   make([]ReportResult, len(results)),
	}
	for i, r := range results {
		body.Results[i] = NewReportResult(r)
	}
	return Report{
		Version: ReportVersion,
		Schema:  ReportSchema,
		Report:  body,
	}
}
