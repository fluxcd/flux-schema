// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// ConfigKind is the kind used by validation configuration files.
	ConfigKind = "Config"
)

// Config defines validation configuration.
//
// +kubebuilder:object:root=true
type Config struct {
	// TypeMeta identifies the config API version and kind.
	metav1.TypeMeta `json:",inline"`

	// Validate contains defaults for the validate command.
	Validate ValidateConfig `json:"validate"`
}

// ValidateConfig defines defaults for validation options.
type ValidateConfig struct {
	// SchemaLocations contains schema URLs, file paths, or templates to try in order.
	// +optional
	SchemaLocations []string `json:"schemaLocation,omitempty"`

	// SkipMissingSchemas skips documents for which no schema can be found.
	// +optional
	SkipMissingSchemas bool `json:"skipMissingSchemas,omitempty"`

	// SkipKinds contains kind or apiVersion/kind patterns excluded from validation.
	// +optional
	SkipKinds []string `json:"skipKind,omitempty"`

	// SkipJSONPaths contains JSON Pointers stripped before validation.
	// +optional
	SkipJSONPaths []string `json:"skipJSONPath,omitempty"`

	// SkipFiles contains basename glob patterns excluded from validation.
	// +optional
	SkipFiles []string `json:"skipFile,omitempty"`

	// SkipCELRules disables evaluation of x-kubernetes-validations CEL rules.
	// +optional
	SkipCELRules bool `json:"skipCELRules,omitempty"`

	// Verbose prints a line for every document, including valid and skipped documents.
	// +optional
	Verbose bool `json:"verbose,omitempty"`

	// FailFast exits after the first invalid document.
	// +optional
	FailFast bool `json:"failFast,omitempty"`

	// Concurrent is the number of concurrent validation workers.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Concurrent *int `json:"concurrent,omitempty"`

	// InsecureSkipTLSVerify disables TLS certificate verification for HTTPS schemas.
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify,omitempty"`

	// Output selects the command output format.
	// +optional
	Output ConfigOutput `json:"output,omitempty"`
}

// ConfigOutput is a supported validate command output format.
//
// +k8s:enum
type ConfigOutput string

const (
	// ConfigOutputText emits human-readable text.
	ConfigOutputText ConfigOutput = "text"

	// ConfigOutputJSON emits a JSON report.
	ConfigOutputJSON ConfigOutput = "json"

	// ConfigOutputYAML emits a YAML report.
	ConfigOutputYAML ConfigOutput = "yaml"
)
