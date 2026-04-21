// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package tmpl renders Go text/template strings with the CRD schema
// variables shared across flux-schema commands.
package tmpl

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// SchemaVars carries the variables exposed to output-path templates.
// All fields are lowercased by Render before rendering, and GroupPrefix
// is derived from Group when empty.
type SchemaVars struct {
	Group       string
	GroupPrefix string
	Kind        string
	Version     string
}

// Parse compiles format into a reusable *template.Template. Unknown
// template variables produce an error instead of an empty string
// (missingkey=error). Use Execute to render the compiled template.
func Parse(format string) (*template.Template, error) {
	if strings.TrimSpace(format) == "" {
		return nil, fmt.Errorf("output format template is empty")
	}
	tpl, err := template.New("output").Option("missingkey=error").Parse(format)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return tpl, nil
}

// Execute renders a pre-compiled template with vars. Inputs are lowercased
// and GroupPrefix is derived from Group (first dot-delimited segment) when
// unset.
func Execute(tpl *template.Template, vars SchemaVars) (string, error) {
	normalised := SchemaVars{
		Group:       strings.ToLower(vars.Group),
		GroupPrefix: strings.ToLower(vars.GroupPrefix),
		Kind:        strings.ToLower(vars.Kind),
		Version:     strings.ToLower(vars.Version),
	}
	if normalised.GroupPrefix == "" {
		prefix, _, _ := strings.Cut(normalised.Group, ".")
		normalised.GroupPrefix = prefix
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, normalised); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// Render is a convenience that parses format and executes it in one call.
// Callers with a hot loop should use Parse once and Execute per call.
func Render(format string, vars SchemaVars) (string, error) {
	tpl, err := Parse(format)
	if err != nil {
		return "", err
	}
	return Execute(tpl, vars)
}
