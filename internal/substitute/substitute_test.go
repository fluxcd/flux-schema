// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package substitute

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestBytes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		raw    string
		vars   map[string]string
		strict bool
		want   string
		reason string
		msg    string
	}{
		{name: "disabled", raw: "# keep\nvalue: '${VAR}'\n", want: "# keep\nvalue: '${VAR}'\n"},
		{name: "no dollar", raw: "# keep\nvalue: '2'\n", vars: map[string]string{}, want: "# keep\nvalue: '2'\n"},
		{name: "no dollar avoids parsing", raw: "value: [\n", vars: map[string]string{}, want: "value: [\n"},
		{name: "nil avoids parsing", raw: "value: [${VAR}\n", want: "value: [${VAR}\n"},
		{name: "integer", raw: "value: \"${VAR}\"\n", vars: map[string]string{"VAR": "2"}, want: "value: 2\n"},
		{name: "boolean", raw: "value: '${VAR}'\n", vars: map[string]string{"VAR": "true"}, want: "value: true\n"},
		{name: "newlines", raw: "value: '${VAR}'\n", vars: map[string]string{"VAR": "a\nb\n"}, want: "value: ab\n"},
		{name: "empty enables defaults", raw: "value: '${VAR:=fallback}'\n", vars: map[string]string{}, want: "value: fallback\n"},
		{name: "ignores process environment", raw: "value: 'prefix-${VAR}'\n", vars: map[string]string{}, want: "value: prefix-\n"},
		{name: "literal dollars in values", raw: "value: '${VAR}'\n", vars: map[string]string{"VAR": "${OTHER}"}, want: "value: ${OTHER}\n"},
		{name: "escape", raw: "value: '$${VAR}'\n", vars: map[string]string{}, strict: true, want: "value: ${VAR}\n"},
		{name: "strict empty value", raw: "value: 'prefix-${VAR}'\n", vars: map[string]string{"VAR": ""}, strict: true, want: "value: prefix-\n"},
		{name: "strict default", raw: "value: '${VAR:=fallback}'\n", vars: map[string]string{}, strict: true, want: "value: fallback\n"},
		{name: "comments not substituted", raw: "# ${\nvalue: '${VAR}' # ${MISSING}\n", vars: map[string]string{"VAR": "ok"}, strict: true, want: "value: ok\n"},
		{name: "strict missing", raw: "value: '${VAR}'\n", vars: map[string]string{}, strict: true, reason: ReasonEnvsubstError, msg: `variable not set (strict mode): "VAR"`},
		{name: "malformed expression", raw: "value: '${'\n", vars: map[string]string{}, reason: ReasonEnvsubstError, msg: "variable substitution failed"},
		{name: "duplicate input key", raw: "value: '${VAR}'\nvalue: again\n", vars: map[string]string{}, reason: ReasonYAMLParseError, msg: "already set in map"},
		{name: "unparseable input", raw: "value: [${VAR}\n", vars: map[string]string{}, reason: ReasonYAMLParseError, msg: "yaml:"},
		{name: "unparseable result", raw: "value: '${VAR}'\n", vars: map[string]string{"VAR": "["}, reason: ReasonYAMLParseError, msg: "yaml:"},
		{name: "duplicate substituted key", raw: "${VAR}: first\nexisting: second\n", vars: map[string]string{"VAR": "existing"}, reason: ReasonYAMLParseError, msg: "already set in map"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Setenv("VAR", "must-not-be-used")
			s, err := New(tt.vars, tt.strict)
			g.Expect(err).NotTo(HaveOccurred())
			got, subErr := s.Bytes([]byte(tt.raw))
			if tt.reason != "" {
				g.Expect(subErr).To(HaveOccurred())
				g.Expect(subErr.Reason).To(Equal(tt.reason))
				g.Expect(subErr.Error()).To(ContainSubstring(tt.msg))
				g.Expect(got).To(BeNil())
			} else {
				g.Expect(subErr).NotTo(HaveOccurred())
				g.Expect(string(got)).To(Equal(tt.want))
			}
		})
	}
}

func TestDisabledMarkers(t *testing.T) {
	for _, field := range []string{"labels", "annotations"} {
		t.Run(field, func(t *testing.T) {
			g := NewWithT(t)
			raw := "# keep\nmetadata:\n  " + field + ":\n    kustomize.toolkit.fluxcd.io/substitute: disabled\nvalue: '${'\n"
			s, err := New(map[string]string{}, true)
			g.Expect(err).NotTo(HaveOccurred())
			got, subErr := s.Bytes([]byte(raw))
			g.Expect(subErr).NotTo(HaveOccurred())
			g.Expect(string(got)).To(Equal(raw))
		})
	}
}

func TestVariableNames(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{name: "VAR", valid: true},
		{name: "_", valid: true},
		{name: "_var_2", valid: true},
		{name: ""},
		{name: "2VAR"},
		{name: "VAR.NAME"},
		{name: "VAR-NAME"},
		{name: "VAR NAME"},
		{name: "VAR\n"},
		{name: "\u00e9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := New(map[string]string{tt.name: "value"}, false)
			if tt.valid {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring("var name is invalid")))
			}
		})
	}
}

func TestApplyPreservesTypesAndCopiesVariables(t *testing.T) {
	g := NewWithT(t)
	vars := map[string]string{"VAR": "2"}
	s, err := New(vars, false)
	g.Expect(err).NotTo(HaveOccurred())
	vars["VAR"] = "changed"
	raw, doc, subErr := s.Apply(map[string]any{"value": "${VAR}"})
	g.Expect(subErr).NotTo(HaveOccurred())
	g.Expect(string(raw)).To(Equal("value: 2\n"))
	g.Expect(doc).To(Equal(map[string]any{"value": int64(2)}))
}

func TestReadFile(t *testing.T) {
	g := NewWithT(t)
	vars, err := ReadFile("")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(vars).To(BeNil())

	path := filepath.Join(t.TempDir(), ".env")
	g.Expect(os.WriteFile(path, []byte("A=\"1\"\n"), 0o644)).To(Succeed())
	vars, err = ReadFile(path)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(vars).To(Equal(map[string]string{"A": `"1"`}))

	_, err = ReadFile(filepath.Join(t.TempDir(), "missing.env"))
	g.Expect(err).To(MatchError(ContainSubstring("envsubst: read dotenv file")))
	g.Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
}

func TestBytesErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		raw    string
		vars   map[string]string
		reason string
		msg    string
	}{
		{name: "invalid input", raw: "value: [${A}", vars: map[string]string{}, reason: ReasonYAMLParseError, msg: "did not find expected"},
		{name: "duplicate key", raw: "a: ${A}\na: b", vars: map[string]string{}, reason: ReasonYAMLParseError, msg: `key "a" already set in map`},
		{name: "invalid substituted YAML", raw: "a: ${A}", vars: map[string]string{"A": "["}, reason: ReasonYAMLParseError, msg: "did not find expected"},
		{name: "malformed expression", raw: "a: '${'", vars: map[string]string{}, reason: ReasonEnvsubstError, msg: "variable substitution failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			s, err := New(tt.vars, false)
			g.Expect(err).NotTo(HaveOccurred())
			_, subErr := s.Bytes([]byte(tt.raw))
			g.Expect(subErr).To(HaveOccurred())
			g.Expect(subErr.Reason).To(Equal(tt.reason))
			g.Expect(errors.Unwrap(subErr)).To(Equal(subErr.Err))
			g.Expect(strings.Join(YAMLErrors(subErr), "\n")).To(ContainSubstring(tt.msg))
		})
	}
}

func TestYAMLErrors(t *testing.T) {
	g := NewWithT(t)
	g.Expect(YAMLErrors(errors.New("error converting YAML to JSON: yaml: line 1: bad"))).To(Equal([]string{"line 1: bad"}))
	g.Expect(YAMLErrors(errors.New("yaml: unmarshal errors:\n  line 2: dup a\n  line 3: dup b"))).
		To(Equal([]string{"line 2: dup a", "line 3: dup b"}))
	g.Expect(YAMLErrors(errors.New("other"))).To(Equal([]string{"other"}))
}
