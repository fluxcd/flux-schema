// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// ReportKind is the kind used by validation report envelopes.
	ReportKind = "Report"

	// ReportSchema is the canonical URL of the JSON Schema describing the Report shape.
	ReportSchema = "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/report/report-v1beta1.json"
)

// Report is the validation report envelope.
//
// +kubebuilder:object:root=true
type Report struct {
	// TypeMeta identifies the report API version and kind.
	metav1.TypeMeta `json:",inline"`

	// Schema identifies the JSON Schema for this report.
	// +optional
	Schema string `json:"$schema,omitempty"`

	// Report contains the validation report data.
	Report ReportSpec `json:"report"`
}

// ReportSpec contains validation report details.
type ReportSpec struct {
	// Reporter identifies the software that produced the report.
	// +kubebuilder:validation:Pattern=`^flux-schema/`
	Reporter string `json:"reporter"`

	// Timestamp is the report creation time in RFC 3339 format.
	// +kubebuilder:validation:Format=date-time
	Timestamp string `json:"timestamp"`

	// Summary contains aggregate validation counts.
	Summary ReportSummary `json:"summary"`

	// Results contains one validation result per processed document.
	Results []ReportResult `json:"results"`
}

// ReportSummary contains aggregate validation counts.
type ReportSummary struct {
	// Total is the number of processed documents.
	// +kubebuilder:validation:Minimum=0
	Total int `json:"total"`

	// Valid is the number of documents that passed validation.
	// +kubebuilder:validation:Minimum=0
	Valid int `json:"valid"`

	// Invalid is the number of documents that failed validation.
	// +kubebuilder:validation:Minimum=0
	Invalid int `json:"invalid"`

	// Skipped is the number of documents that were not validated.
	// +kubebuilder:validation:Minimum=0
	Skipped int `json:"skipped"`
}

// ReportResult is the wire form of a single validated document.
type ReportResult struct {
	// Resource identifies the validated document. It is null when no identity
	// could be recovered.
	// +kubebuilder:validation:Required
	// +nullable
	Resource *ReportResource `json:"resource"`

	// Source identifies the input source for the document.
	Source string `json:"source"`

	// Idx is the zero-based document index within the source.
	// +kubebuilder:validation:Minimum=0
	Idx int `json:"idx"`

	// Status is the validation outcome for the document.
	// +kubebuilder:validation:Enum=valid;invalid;skipped
	Status string `json:"status"`

	// Reason is a stable machine-readable code for non-valid results.
	// +optional
	Reason ReportReason `json:"reason,omitempty"`

	// Violations contains validation errors found in the document.
	// +optional
	Violations []ReportViolation `json:"violations,omitempty"`
}

// ReportReason is a stable report result reason code.
//
// +k8s:enum
type ReportReason string

const (
	// ReportReasonSourceLoadError means the input source could not be loaded.
	ReportReasonSourceLoadError ReportReason = "source-load-error"

	// ReportReasonYAMLParseError means the document could not be parsed as YAML.
	ReportReasonYAMLParseError ReportReason = "yaml-parse-error"

	// ReportReasonKindSkipped means the document kind was excluded from validation.
	ReportReasonKindSkipped ReportReason = "kind-skipped"

	// ReportReasonSchemaLoadError means the schema catalog could not be loaded.
	ReportReasonSchemaLoadError ReportReason = "schema-load-error"

	// ReportReasonSchemaNotFound means no schema was found for the document.
	ReportReasonSchemaNotFound ReportReason = "schema-not-found"

	// ReportReasonSchemaViolation means the document failed JSON Schema validation.
	ReportReasonSchemaViolation ReportReason = "schema-violation"

	// ReportReasonCELViolation means the document failed CEL validation.
	ReportReasonCELViolation ReportReason = "cel-violation"
)

// ReportResource identifies a validated document.
type ReportResource struct {
	// APIVersion is the Kubernetes API version of the document.
	APIVersion string `json:"apiVersion"`

	// Kind is the Kubernetes kind of the document.
	Kind string `json:"kind"`

	// Namespace is the Kubernetes namespace of the document.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the Kubernetes name of the document.
	// +optional
	Name string `json:"name,omitempty"`
}

// ReportViolation carries a JSON Pointer path only for schema violations;
// for every other reason Path is empty and Message holds the raw error text.
type ReportViolation struct {
	// Path is the JSON Pointer to the invalid field for schema violations.
	// +optional
	Path string `json:"path,omitempty"`

	// Message describes the validation error.
	Message string `json:"message"`
}
