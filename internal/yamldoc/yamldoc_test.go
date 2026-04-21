// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package yamldoc

import (
	"bufio"
	"bytes"
	"testing"

	. "github.com/onsi/gomega"
)

func scanAll(t *testing.T, input string) []string {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader([]byte(input)))
	scanner.Split(SplitDocument)
	var out []string
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return out
}

func TestSplitDocument_Single(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "foo: bar\nbaz: qux\n")
	g.Expect(docs).To(HaveLen(1))
	g.Expect(docs[0]).To(Equal("foo: bar\nbaz: qux\n"))
}

func TestSplitDocument_MultiDoc(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "foo: 1\n---\nfoo: 2\n---\nfoo: 3\n")
	g.Expect(docs).To(Equal([]string{"foo: 1", "foo: 2", "foo: 3\n"}))
}

func TestSplitDocument_LeadingSeparator(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "---\nfoo: 1\n---\nfoo: 2\n")
	// A leading "---" with no preceding newline is kept inside the first
	// token; YAML parsers treat it as a document-start marker.
	g.Expect(docs).To(HaveLen(2))
	g.Expect(docs[0]).To(Equal("---\nfoo: 1"))
	g.Expect(docs[1]).To(Equal("foo: 2\n"))
}

func TestSplitDocument_TrailingSeparator(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "foo: 1\n---")
	g.Expect(docs).To(Equal([]string{"foo: 1"}))
}

func TestSplitDocument_TrailingSeparatorWithNewline(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "foo: 1\n---\n")
	g.Expect(docs).To(Equal([]string{"foo: 1"}))
}

func TestSplitDocument_Empty(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "")
	g.Expect(docs).To(BeEmpty())
}

func TestSplitDocument_OnlySeparator(t *testing.T) {
	g := NewWithT(t)
	docs := scanAll(t, "---\n")
	// A lone "---" is kept as a token; the extractor and validator layers
	// decide whether an empty/null document is meaningful.
	g.Expect(docs).To(Equal([]string{"---\n"}))
}

func TestSplit_AllocatesIndependentBuffers(t *testing.T) {
	g := NewWithT(t)
	input := []byte("foo: 1\n---\nfoo: 2\n")
	docs := Split(input)
	g.Expect(docs).To(HaveLen(2))
	// Mutating the input must not affect the returned slices.
	for i := range input {
		input[i] = 0
	}
	g.Expect(string(docs[0])).To(Equal("foo: 1"))
	g.Expect(string(docs[1])).To(Equal("foo: 2\n"))
}

func TestSplit_Empty(t *testing.T) {
	g := NewWithT(t)
	g.Expect(Split(nil)).To(BeEmpty())
	g.Expect(Split([]byte{})).To(BeEmpty())
}

func TestNewScanner_UsesSplitDocument(t *testing.T) {
	g := NewWithT(t)
	s := NewScanner(bytes.NewReader([]byte("foo: 1\n---\nfoo: 2\n")))
	var got []string
	for s.Scan() {
		got = append(got, s.Text())
	}
	g.Expect(s.Err()).ToNot(HaveOccurred())
	g.Expect(got).To(Equal([]string{"foo: 1", "foo: 2\n"}))
}

func TestSplitDocument_SeparatorInStringLiteral(t *testing.T) {
	g := NewWithT(t)
	// Deliberate edge case: the splitter is line-oriented, not YAML-aware,
	// so "\n---" inside a block scalar still splits. This matches upstream
	// Kubernetes behavior and is documented in the package comment.
	input := "data: |\n  line1\n---\nnext: doc\n"
	docs := scanAll(t, input)
	g.Expect(docs).To(Equal([]string{"data: |\n  line1", "next: doc\n"}))
}
