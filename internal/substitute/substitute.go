// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package substitute applies Flux post-build variable substitution.
package substitute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/fluxcd/pkg/envsubst"
	utiljson "k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"

	"github.com/fluxcd/flux-schema/internal/dotenv"
)

const (
	ReasonEnvsubstError  = "envsubst-error"
	ReasonYAMLParseError = "yaml-parse-error"
)

// Error distinguishes substitution failures from strict YAML decoding errors.
type Error struct {
	Reason string
	Err    error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Substituter holds an immutable copy of the post-build variables.
type Substituter struct {
	vars   map[string]string
	strict bool
}

// ReadFile loads literal dotenv variables. An empty path disables substitution.
func ReadFile(path string) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	vars, err := dotenv.Read(path)
	if err != nil {
		return nil, fmt.Errorf("envsubst: read dotenv file %s: %w", path, err)
	}
	return vars, nil
}

// New validates variable names and strips newlines from values, like Flux.
// Nil vars disables substitution; an empty non-nil map still expands defaults.
func New(vars map[string]string, strict bool) (*Substituter, error) {
	const varNamePattern = "^[_[:alpha:]][_[:alpha:][:digit:]]*$"
	s := &Substituter{strict: strict}
	if vars != nil {
		validName := regexp.MustCompile(varNamePattern)
		s.vars = make(map[string]string, len(vars))
		for name, value := range vars {
			if !validName.MatchString(name) {
				return nil, fmt.Errorf("envsubst: %q var name is invalid, must match %q", name, varNamePattern)
			}
			s.vars[name] = strings.ReplaceAll(value, "\n", "")
		}
	}
	return s, nil
}

func (s *Substituter) needsDecode(raw []byte) bool {
	return s.vars != nil && bytes.Contains(raw, []byte("$"))
}

// Applies checks the variable shortcut and the resource's opt-out markers.
// Callers that filter kinds can do so between Applies and Apply.
func (s *Substituter) Applies(raw []byte, doc map[string]any) bool {
	if !s.needsDecode(raw) {
		return false
	}
	metadata, _ := doc["metadata"].(map[string]any)
	for _, field := range []string{"labels", "annotations"} {
		values, _ := metadata[field].(map[string]any)
		if values["kustomize.toolkit.fluxcd.io/substitute"] == "disabled" {
			return false
		}
	}
	return true
}

// Bytes substitutes raw only when enabled and applicable. Documents that
// are not substituted retain their original bytes, including comments.
func (s *Substituter) Bytes(raw []byte) ([]byte, *Error) {
	if !s.needsDecode(raw) {
		return raw, nil
	}
	doc, err := Decode(raw, true)
	if err != nil {
		return nil, &Error{Reason: ReasonYAMLParseError, Err: err}
	}
	if !s.Applies(raw, doc) {
		return raw, nil
	}
	output, _, subErr := s.Apply(doc)
	return output, subErr
}

// Apply re-serializes a decoded document, substitutes variables, and strictly
// decodes the result. Quoted "${VAR}" takes the type of its substituted value.
// Both the rendered bytes and the decoded object represent the same payload.
func (s *Substituter) Apply(doc map[string]any) ([]byte, map[string]any, *Error) {
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, &Error{Reason: ReasonEnvsubstError, Err: err}
	}
	data, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return nil, nil, &Error{Reason: ReasonEnvsubstError, Err: err}
	}
	output, err := envsubst.Eval(string(data), func(name string) (string, bool) {
		value, ok := s.vars[name]
		return value, ok || !s.strict
	})
	if err != nil {
		return nil, nil, &Error{Reason: ReasonEnvsubstError, Err: fmt.Errorf("variable substitution failed: %w", err)}
	}
	raw := []byte(output)
	substituted, err := Decode(raw, true)
	if err != nil {
		return nil, nil, &Error{Reason: ReasonYAMLParseError, Err: err}
	}
	return raw, substituted, nil
}

// Decode parses YAML into an object, preserving integer types for Kubernetes
// CEL evaluation. Strict decoding rejects duplicate YAML keys.
func Decode(raw []byte, strict bool) (map[string]any, error) {
	var (
		jsonBytes []byte
		err       error
	)
	if strict {
		jsonBytes, err = yaml.YAMLToJSONStrict(raw)
	} else {
		jsonBytes, err = yaml.YAMLToJSON(raw)
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := utiljson.Unmarshal(jsonBytes, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// YAMLErrors removes decoder prefixes and separates strict decode violations.
func YAMLErrors(err error) []string {
	const (
		multiPrefixWrapped = "error converting YAML to JSON: yaml: unmarshal errors:"
		multiPrefixBare    = "yaml: unmarshal errors:"
		singlePrefix       = "error converting YAML to JSON: yaml: "
		rawPrefix          = "yaml: "
	)
	msg := err.Error()
	for _, prefix := range []string{multiPrefixWrapped, multiPrefixBare} {
		rest, ok := strings.CutPrefix(msg, prefix)
		if !ok {
			continue
		}
		var out []string
		for line := range strings.SplitSeq(rest, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				out = append(out, t)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	msg = strings.TrimPrefix(msg, singlePrefix)
	msg = strings.TrimPrefix(msg, rawPrefix)
	return []string{strings.TrimSpace(msg)}
}
