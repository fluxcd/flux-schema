// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
)

func TestEcosystemIndexResolveAndComplete(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = fmt.Fprint(w, `{"v":2,"projects":[{"groups":[{"g":"helm.toolkit.fluxcd.io","kinds":[["helmrelease",["v2"],1,"HelmRelease"]]}]}]}`)
		case "/catalog/helm.toolkit.fluxcd.io/helmrelease_v2.json":
			_, _ = fmt.Fprint(w, `{
				"description":"HelmRelease is the Schema for the helmreleases API",
				"properties":{
					"apiVersion":{"type":"string"},
					"kind":{"type":"string"},
					"spec":{"type":"object","description":"HelmReleaseSpec defines the desired state of a Helm release."}
				},
				"type":"object"
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ex, err := New(Options{
		SchemaLocations: []string{srv.URL + "/catalog/{{.Group}}/{{.Kind}}_{{.Version}}.json"},
		IndexLocations:  []string{srv.URL + "/index.json"},
	})
	g.Expect(err).ToNot(HaveOccurred())

	matches, err := ex.CompleteResourceNames(context.Background(), "hr")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{"helmreleases.helm.toolkit.fluxcd.io"}))

	var out bytes.Buffer
	g.Expect(ex.Explain(context.Background(), "hr.spec", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("GROUP:      helm.toolkit.fluxcd.io\n"))
	g.Expect(out.String()).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out.String()).To(ContainSubstring("VERSION:    v2\n"))
	g.Expect(out.String()).To(ContainSubstring("FIELD: spec <Object>\n"))

	out.Reset()
	g.Expect(ex.Explain(context.Background(), "helmreleases.helm.toolkit.fluxcd.io.spec", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("KIND:       HelmRelease\n"))
	g.Expect(out.String()).To(ContainSubstring("FIELD: spec <Object>\n"))
}

func TestCatalogIndexKindDecode(t *testing.T) {
	g := NewWithT(t)

	var kind catalogIndexKind
	g.Expect(kind.UnmarshalJSON([]byte(`["ocirepository",["v1"],1,"OCIRepository"]`))).To(Succeed())
	g.Expect(kind.Name).To(Equal("ocirepository"))
	g.Expect(kind.Versions).To(Equal([]string{"v1"}))
	g.Expect(kind.Kind).To(Equal("OCIRepository"))
}
