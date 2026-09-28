// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestKustomizeProducerOrder(t *testing.T) {
	g := NewWithT(t)
	r, err := New(Options{})
	g.Expect(err).NotTo(HaveOccurred())
	var want []Event
	for range 5 {
		var got []Event
		var sources []string
		g.Expect(r.Walk(context.Background(), []string{"../validator/testdata/kustomize/mixed"}, func(e Event) {
			if e.Final {
				sources = append(sources, e.Source)
			}
			got = append(got, e)
		})).To(Succeed())
		g.Expect(sources).To(Equal([]string{
			"../validator/testdata/kustomize/mixed/overlay",
			"../validator/testdata/kustomize/mixed/plain.yaml",
		}))
		g.Expect(got).To(HaveLen(6))
		g.Expect(got[0].Origin).To(Equal("../validator/testdata/kustomize/mixed/overlay/base/nested/widgets.yaml"))
		g.Expect(got[2].Origin).To(BeEmpty())
		g.Expect(got[3].Final).To(BeTrue())
		g.Expect(got[4].SourceIndex).To(Equal(1))
		g.Expect(got[4].DocIndex).To(Equal(1))
		if want == nil {
			want = got
		}
		g.Expect(got).To(Equal(want))
	}
}

func TestKustomizeCancellation(t *testing.T) {
	g := NewWithT(t)
	r, err := New(Options{})
	g.Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sources := 0
	err = r.Walk(ctx, []string{"../validator/testdata/kustomize"}, func(e Event) {
		g.Expect(e.Source).To(ContainSubstring("component"))
		if e.Final {
			sources++
			cancel()
		}
	})
	g.Expect(err).To(MatchError(context.Canceled))
	g.Expect(sources).To(Equal(1))
}

func TestWalk(t *testing.T) {
	const root = "../validator/testdata/kustomize/"
	for _, tt := range []struct {
		name  string
		path  string
		skip  []string
		docs  int
		final int
	}{
		{name: "nested build", path: root + "mixed", docs: 4, final: 2},
		{name: "explicit kustomization", path: root + "mixed/overlay/kustomization.yaml", docs: 3, final: 1},
		{name: "explicit extensionless kustomization", path: root + "external/Kustomization", docs: 1, final: 1},
		{name: "skip kustomization", path: root + "mixed/overlay", skip: []string{"kustomization.yaml"}, docs: 4, final: 3},
		{name: "skip explicit file renders as YAML", path: root + "mixed/overlay/kustomization.yaml", skip: []string{"kustomization.yaml"}, docs: 1, final: 1},
		{name: "skip directory", path: root + "mixed", skip: []string{"overlay"}, docs: 1, final: 1},
		{name: "empty build", path: root + "empty", final: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			r, err := New(Options{SkipFiles: tt.skip})
			g.Expect(err).NotTo(HaveOccurred())
			docs, finals := 0, 0
			g.Expect(r.Walk(context.Background(), []string{tt.path}, func(e Event) {
				g.Expect(e.Err).NotTo(HaveOccurred())
				g.Expect(e.SourceIndex).To(Equal(finals))
				if e.Final {
					finals++
				} else {
					docs++
				}
			})).To(Succeed())
			g.Expect(docs).To(Equal(tt.docs))
			g.Expect(finals).To(Equal(tt.final))
		})
	}
}

func TestWalkSkipFiles(t *testing.T) {
	for _, tt := range []struct {
		name string
		skip []string
		want []string
	}{
		{name: "default", want: []string{"a.YAML", "b.yml"}},
		{name: "empty", skip: []string{}, want: []string{".hidden.yaml", ".private/c.yaml", "a.YAML", "b.yml"}},
		{name: "custom replaces default", skip: []string{"a.*", ".private"}, want: []string{".hidden.yaml", "b.yml"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			g.Expect(os.Mkdir(filepath.Join(dir, ".private"), 0o755)).To(Succeed())
			for _, name := range []string{".hidden.yaml", ".private/c.yaml", "a.YAML", "b.yml", "ignored.txt"} {
				g.Expect(os.WriteFile(filepath.Join(dir, name), []byte("value: true\n"), 0o644)).To(Succeed())
			}
			r, err := New(Options{SkipFiles: tt.skip})
			g.Expect(err).NotTo(HaveOccurred())
			var got []string
			g.Expect(r.Walk(context.Background(), []string{dir}, func(e Event) {
				g.Expect(e.Err).NotTo(HaveOccurred())
				if !e.Final {
					rel, err := filepath.Rel(dir, e.Source)
					g.Expect(err).NotTo(HaveOccurred())
					got = append(got, filepath.ToSlash(rel))
				}
			})).To(Succeed())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestWalkRawDocuments(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  []string
	}{
		{name: "comments and whitespace", input: " \n# header\nvalue: 'x'  \n\n", want: []string{"# header\nvalue: 'x'"}},
		{name: "document newlines", input: "first: 1\n\n---\nsecond: 2\n---\n", want: []string{"first: 1", "second: 2"}},
		{name: "content-free preamble", input: "# comment\n---\na: 1\n---\n # empty\n---\nb: 2", want: []string{"a: 1", "b: 2"}},
		{name: "empty", input: " \n# nothing\n"},
		{name: "CRLF", input: "a: 1\r\n---\r\nb: 2\r\n", want: []string{"a: 1", "b: 2"}},
		{name: "surrounding whitespace trimmed", input: "x: 0\n---\n\n\n  a: 1\n  b: 2\n\n", want: []string{"x: 0", "a: 1\n  b: 2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			r, err := New(Options{Stdin: strings.NewReader(tt.input)})
			g.Expect(err).NotTo(HaveOccurred())
			var got []string
			finals := 0
			g.Expect(r.Walk(context.Background(), []string{StdinSource}, func(e Event) {
				g.Expect(e.Source).To(Equal(StdinSource))
				g.Expect(e.Err).NotTo(HaveOccurred())
				if e.Final {
					finals++
				} else {
					got = append(got, string(e.Raw))
					g.Expect(e.DocIndex).To(Equal(len(got)))
				}
			})).To(Succeed())
			g.Expect(got).To(Equal(tt.want))
			g.Expect(finals).To(Equal(1))
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("source read failed")
}

func TestWalkFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		path   string
		stdin  io.Reader
		reason string
		msg    string
	}{
		{name: "missing file", path: "missing.yaml", reason: ReasonSourceLoadError, msg: "no such file"},
		{name: "missing stdin", path: StdinSource, reason: ReasonSourceLoadError, msg: `source "stdin" requires Options.Stdin to be set`},
		{name: "stdin read", path: StdinSource, stdin: failingReader{}, reason: ReasonSourceLoadError, msg: "scan stdin: source read failed"},
		{name: "broken build", path: "../../cmd/flux-schema/testdata/validate/kustomize/broken", reason: ReasonKustomizeBuildError, msg: "missing.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			r, err := New(Options{Stdin: tt.stdin})
			g.Expect(err).NotTo(HaveOccurred())
			var got []Event
			g.Expect(r.Walk(context.Background(), []string{tt.path, "../validator/testdata/kustomize/mixed/plain.yaml"}, func(e Event) {
				got = append(got, e)
			})).To(Succeed())
			g.Expect(got).To(HaveLen(4))
			g.Expect(got[0].Reason).To(Equal(tt.reason))
			g.Expect(got[0].Err).To(MatchError(ContainSubstring(tt.msg)))
			g.Expect(got[0].DocIndex).To(BeZero())
			g.Expect(got[1]).To(Equal(Event{Source: tt.path, Final: true}))
			g.Expect(got[2].SourceIndex).To(Equal(1))
			g.Expect(got[2].Raw).NotTo(BeEmpty())
			g.Expect(got[3].Final).To(BeTrue())
		})
	}
}

func TestSkipFilePatterns(t *testing.T) {
	for _, pattern := range []string{"", " ", "["} {
		t.Run(pattern, func(t *testing.T) {
			g := NewWithT(t)
			_, err := New(Options{SkipFiles: []string{pattern}})
			g.Expect(err).To(MatchError(ContainSubstring("skip file pattern")))
		})
	}
}

func TestWalkFileReadFailure(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "a.yaml")
	g.Expect(os.Symlink(filepath.Join(dir, "missing"), bad)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("name: after\n"), 0o644)).To(Succeed())
	r, err := New(Options{})
	g.Expect(err).NotTo(HaveOccurred())
	var got []Event
	g.Expect(r.Walk(context.Background(), []string{dir}, func(e Event) {
		got = append(got, e)
	})).To(Succeed())
	g.Expect(got).To(HaveLen(4))
	g.Expect(got[0].Source).To(Equal(bad))
	g.Expect(got[0].Reason).To(Equal(ReasonSourceLoadError))
	g.Expect(got[0].Err).To(HaveOccurred())
	g.Expect(got[1].Final).To(BeTrue())
	g.Expect(string(got[2].Raw)).To(Equal("name: after"))
	g.Expect(got[3].Final).To(BeTrue())
}

func TestIsContentFree(t *testing.T) {
	cases := map[string]bool{
		"":                                    true,
		"   \n\t\n":                           true,
		"# a comment":                         true,
		"# line 1\n# line 2\n":                true,
		"  # indented comment\n\n# another\n": true,
		"apiVersion: v1":                      false,
		"# comment\napiVersion: v1":           false,
		"---":                                 false,
		`value: "# not a comment"`:            false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsContentFree([]byte(in))).To(Equal(want))
		})
	}
}
