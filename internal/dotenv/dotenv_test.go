// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	. "github.com/onsi/gomega"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{name: "empty", input: "", want: map[string]string{}},
		{name: "literal values", input: "A=1\nB=hello world\nC=x#y\n",
			want: map[string]string{"A": "1", "B": "hello world", "C": "x#y"}},
		{name: "quotes are kept", input: "A=\"quoted\"\nB='single'\n",
			want: map[string]string{"A": `"quoted"`, "B": "'single'"}},
		{name: "references are not expanded", input: "A=1\nB=$A\nC=$HOME.example.com\nD=${A}\n",
			want: map[string]string{"A": "1", "B": "$A", "C": "$HOME.example.com", "D": "${A}"}},
		{name: "commands are not run", input: "A=$(touch pwned)\nB=`id`\n",
			want: map[string]string{"A": "$(touch pwned)", "B": "`id`"}},
		{name: "value keeps equals and whitespace", input: "A=b=c \nB=\n",
			want: map[string]string{"A": "b=c ", "B": ""}},
		{name: "comments and blank lines", input: "# header\n\n  # indented\n \t\nA=1\n",
			want: map[string]string{"A": "1"}},
		{name: "leading whitespace", input: "  A=1\n\tB=2\n", want: map[string]string{"A": "1", "B": "2"}},
		{name: "CRLF and BOM", input: "\xef\xbb\xbfA=1\r\nB=2\r\n", want: map[string]string{"A": "1", "B": "2"}},
		{name: "only the CRLF carriage return is removed", input: "A=1\r\r\nB=2\r", want: map[string]string{"A": "1\r", "B": "2\r"}},
		{name: "no final newline", input: "A=1\nB=2", want: map[string]string{"A": "1", "B": "2"}},
		{name: "BOM only on the first line", input: "A=1\n\ufeffB=2\n", wantErr: `line 2: invalid variable name "\ufeffB"`},
		{name: "long value", input: "A=" + strings.Repeat("x", 2<<20) + "\n", want: map[string]string{"A": strings.Repeat("x", 2<<20)}},
		{name: "non-ASCII whitespace is not skipped", input: "\u00a0A=1\n", wantErr: `line 1: invalid variable name "\u00a0A"`},
		{name: "last definition wins", input: "A=1\nA=2\n", want: map[string]string{"A": "2"}},
		{name: "missing equals", input: "A=1\nB\n", wantErr: "line 2: expected a variable assignment"},
		{name: "export prefix", input: "export A=1\n", wantErr: `line 1: invalid variable name "export A"`},
		{name: "space before equals", input: "A =1\n", wantErr: `line 1: invalid variable name "A "`},
		{name: "invalid name", input: "# c\nA.B=1\n", wantErr: `line 2: invalid variable name "A.B"`},
		{name: "empty name", input: "=1\n", wantErr: `line 1: invalid variable name ""`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got, err := Parse(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestRead(t *testing.T) {
	g := NewWithT(t)
	path := filepath.Join(t.TempDir(), ".env")
	g.Expect(os.WriteFile(path, []byte("A=1\n"), 0o644)).To(Succeed())
	got, err := Read(path)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(Equal(map[string]string{"A": "1"}))

	_, err = Read(filepath.Join(t.TempDir(), "missing.env"))
	g.Expect(err).To(MatchError(os.ErrNotExist))
}

func TestParseReadError(t *testing.T) {
	g := NewWithT(t)
	_, err := Parse(iotest.TimeoutReader(strings.NewReader("A=1\nB=2\n")))
	g.Expect(err).To(MatchError(iotest.ErrTimeout))
}
