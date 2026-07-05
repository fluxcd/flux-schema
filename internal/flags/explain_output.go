// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package flags

import "fmt"

// ExplainOutput is a supported explain command output format.
type ExplainOutput string

const (
	// ExplainOutputPlaintext emits kubectl OpenAPI v3 style text.
	ExplainOutputPlaintext ExplainOutput = "plaintext"

	// ExplainOutputPlaintextOpenAPIV2 emits kubectl OpenAPI v2 style text.
	ExplainOutputPlaintextOpenAPIV2 ExplainOutput = "plaintext-openapiv2"
)

// Set validates and assigns the output format.
func (o *ExplainOutput) Set(v string) error {
	switch v {
	case string(ExplainOutputPlaintext), string(ExplainOutputPlaintextOpenAPIV2):
		*o = ExplainOutput(v)
		return nil
	default:
		return fmt.Errorf("must be one of %s", o.Type())
	}
}

// String returns the selected output format.
func (o ExplainOutput) String() string {
	return string(o)
}

// Type returns the pflag type hint.
func (o ExplainOutput) Type() string {
	return "plaintext|plaintext-openapiv2"
}

// Description returns the pflag help text.
func (o ExplainOutput) Description() string {
	return "output format, one of " + o.Type()
}
