// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestValidateSourcesKustomize(t *testing.T) {
	const root = "testdata/kustomize/"
	const overlay = root + "mixed/overlay/"
	const origin = overlay + "base/nested/widgets.yaml"
	const overlayDir = root + "mixed/overlay"
	for _, tt := range []struct {
		name      string
		path      string
		skipFiles []string
		want      []Result
		finals    int
	}{
		{
			name: "mixed tree builds nested directories once",
			path: root + "mixed",
			want: []Result{
				{Source: overlayDir, Origin: origin, DocIndex: 1, Name: "rendered-first", Status: StatusValid},
				{Source: overlayDir, Origin: origin, DocIndex: 2, Name: "rendered-second", Status: StatusInvalid},
				{Source: overlayDir, DocIndex: 3, Name: "rendered-generated", Status: StatusValid},
				{Source: root + "mixed/plain.yaml", DocIndex: 1, Name: "plain", Status: StatusValid},
			},
			finals: 2,
		},
		{
			name: "explicit directory",
			path: overlay,
			want: []Result{
				{Source: overlayDir, Origin: origin, DocIndex: 1, Name: "rendered-first", Status: StatusValid},
				{Source: overlayDir, Origin: origin, DocIndex: 2, Name: "rendered-second", Status: StatusInvalid},
				{Source: overlayDir, DocIndex: 3, Name: "rendered-generated", Status: StatusValid},
			},
			finals: 1,
		},
		{
			name: "explicit kustomization file",
			path: overlay + "kustomization.yaml",
			want: []Result{
				{Source: overlayDir, Origin: origin, DocIndex: 1, Name: "rendered-first", Status: StatusValid},
				{Source: overlayDir, Origin: origin, DocIndex: 2, Name: "rendered-second", Status: StatusInvalid},
				{Source: overlayDir, DocIndex: 3, Name: "rendered-generated", Status: StatusValid},
			},
			finals: 1,
		},
		{
			name: "plain file is not built",
			path: overlay + "base/nested/widgets.yaml",
			want: []Result{
				{Source: origin, DocIndex: 1, Name: "first", Status: StatusValid},
				{Source: origin, DocIndex: 2, Name: "second", Status: StatusValid},
			},
			finals: 1,
		},
		{
			name:      "skip kustomization restores per-file validation",
			path:      overlay,
			skipFiles: []string{"kustomization.yaml"},
			want: []Result{
				{Source: origin, DocIndex: 1, Name: "first", Status: StatusValid},
				{Source: origin, DocIndex: 2, Name: "second", Status: StatusValid},
				{Source: overlay + "patch.yaml", DocIndex: 1, Name: "#1", Status: StatusInvalid},
				{Source: overlay + "unused.yaml", DocIndex: 1, Name: "#1", Status: StatusInvalid},
			},
			finals: 3,
		},
		{
			name:      "skip directory prevents build",
			path:      root + "mixed",
			skipFiles: []string{"overlay"},
			want: []Result{
				{Source: root + "mixed/plain.yaml", DocIndex: 1, Name: "plain", Status: StatusValid},
			},
			finals: 1,
		},
		{
			name: "outside scanned directory",
			path: root + "external",
			want: []Result{
				{Source: root + "external", Origin: root + "shared.yaml", DocIndex: 1, Name: "external", Status: StatusValid},
			},
			finals: 1,
		},
		{
			name: "explicit extensionless kustomization",
			path: root + "external/Kustomization",
			want: []Result{
				{Source: root + "external", Origin: root + "shared.yaml", DocIndex: 1, Name: "external", Status: StatusValid},
			},
			finals: 1,
		},
		{name: "empty build", path: root + "empty/kustomization.yml", finals: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			schemaDir := t.TempDir()
			writeWidgetSchema(t, schemaDir)
			v, err := New(Options{
				SchemaLocations: []string{
					filepath.Join(schemaDir, "{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json"),
					"../../catalog/latest/" + DefaultSchemaLayout,
				},
				SkipFiles: tt.skipFiles,
			})
			g.Expect(err).NotTo(HaveOccurred())
			var got []Result
			finals := map[string]bool{}
			for r := range v.ValidateSources(context.Background(), []string{tt.path}) {
				g.Expect(finals[r.Source]).To(BeFalse(), "result after final sentinel for %s", r.Source)
				if r.Final {
					finals[r.Source] = true
					continue
				}
				got = append(got, Result{
					Source: filepath.ToSlash(r.Source), Origin: filepath.ToSlash(r.Origin),
					DocIndex: r.DocIndex, Name: r.Name, Status: r.Status,
				})
			}
			g.Expect(got).To(ConsistOf(tt.want))
			g.Expect(finals).To(HaveLen(tt.finals))
		})
	}
}

func TestValidateSourcesKustomizeBuildError(t *testing.T) {
	g := NewWithT(t)
	v := newLocalValidator(t, t.TempDir(), true)
	var results []Result
	for r := range v.ValidateSources(context.Background(), []string{"testdata/kustomize/component"}) {
		results = append(results, r)
	}
	g.Expect(results).To(HaveLen(2))
	g.Expect(results[0]).To(Equal(Result{
		Source: "testdata/kustomize/component",
		Status: StatusInvalid, Reason: ReasonKustomizeBuildError, Errors: results[0].Errors,
	}))
	g.Expect(results[0].Errors).To(HaveLen(1))
	g.Expect(results[0].Errors[0].Path).To(BeEmpty())
	g.Expect(results[0].Errors[0].Msg).To(ContainSubstring("no matches"))
	g.Expect(results[1]).To(Equal(Result{Source: results[0].Source, Final: true}))
}

func TestKustomizeProducerOrder(t *testing.T) {
	g := NewWithT(t)
	v := newLocalValidator(t, t.TempDir(), true)
	var want []job
	for range 5 {
		jobs := make(chan job, 10)
		var sources []string
		err := v.produceFromPath(context.Background(), "testdata/kustomize/mixed", jobs, func() *sourceState {
			return &sourceState{}
		}, func(source string, _ *sourceState) {
			sources = append(sources, source)
		})
		g.Expect(err).NotTo(HaveOccurred())
		close(jobs)
		got := make([]job, 0, len(jobs))
		for j := range jobs {
			j.sourceWG.Done()
			j.sourceWG = nil
			got = append(got, j)
		}
		g.Expect(sources).To(Equal([]string{
			"testdata/kustomize/mixed/overlay",
			"testdata/kustomize/mixed/plain.yaml",
		}))
		g.Expect(got).To(HaveLen(4))
		if want == nil {
			want = slices.Clone(got)
		}
		g.Expect(got).To(Equal(want))
	}
}

func TestKustomizeCancellation(t *testing.T) {
	g := NewWithT(t)
	v := newLocalValidator(t, t.TempDir(), true)
	ctx, cancel := context.WithCancel(context.Background())
	jobs := make(chan job, 10)
	sources := 0
	err := v.produceFromPath(ctx, "testdata/kustomize", jobs, func() *sourceState {
		return &sourceState{}
	}, func(_ string, _ *sourceState) {
		sources++
		cancel()
	})
	g.Expect(err).To(MatchError(context.Canceled))
	g.Expect(sources).To(Equal(1))
	close(jobs)
	for j := range jobs {
		j.sourceWG.Done()
		g.Expect(j.source).To(ContainSubstring("component"))
	}
}

func TestKustomizeStdinUnchanged(t *testing.T) {
	g := NewWithT(t)
	v, err := New(Options{
		SchemaLocations: []string{t.TempDir() + "/" + DefaultSchemaLayout},
		Stdin:           strings.NewReader("resources: []\n"),
	})
	g.Expect(err).NotTo(HaveOccurred())
	var results []Result
	for r := range v.ValidateSources(context.Background(), []string{StdinSource}) {
		if !r.Final {
			results = append(results, r)
		}
	}
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Reason).To(Equal(ReasonSchemaViolation))
	g.Expect(results[0].Origin).To(BeEmpty())
}

type failingSourceReader struct{}

func (failingSourceReader) Read([]byte) (int, error) {
	return 0, errors.New("source read failed")
}

func TestValidateSourcesReadFailureOrdering(t *testing.T) {
	g := NewWithT(t)
	v, err := New(Options{
		SchemaLocations: []string{t.TempDir() + "/" + DefaultSchemaLayout},
		Stdin:           failingSourceReader{},
	})
	g.Expect(err).NotTo(HaveOccurred())
	var results []Result
	for r := range v.ValidateSources(context.Background(), []string{StdinSource}) {
		results = append(results, r)
	}
	g.Expect(results).To(HaveLen(2))
	g.Expect(results[0].SourceIndex).To(BeZero())
	g.Expect(results[0].Reason).To(Equal(ReasonSourceLoadError))
	g.Expect(results[0].Errors).To(Equal([]ValidationError{{Msg: "scan stdin: source read failed"}}))
	g.Expect(results[1]).To(Equal(Result{Source: StdinSource, Final: true}))
}
