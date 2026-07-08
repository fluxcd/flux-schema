// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	. "github.com/onsi/gomega"
)

func TestRendererUsesExplainTypeMetadata(t *testing.T) {
	g := NewWithT(t)

	spec := map[string]any{
		"type":                       "object",
		"description":                "Spec configures the pod.",
		keyFluxSchemaType:            "PodSpec",
		keyFluxSchemaTypeDescription: "PodSpec is a description of a pod.",
		"properties":                 map[string]any{},
	}
	root := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": spec,
		},
	}

	var out bytes.Buffer
	r := renderer{w: &out, format: OutputPlaintext}
	g.Expect(r.render(&resolvedSchema{Root: root, Kind: "Pod", Version: "v1"}, []string{"spec"})).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("FIELD: spec <PodSpec>\n"))
	g.Expect(out.String()).To(ContainSubstring("Spec configures the pod."))
	g.Expect(out.String()).To(ContainSubstring("PodSpec is a description of a pod."))
	g.Expect(typeNameV2(spec)).To(Equal("PodSpec"))

	arrayNode := map[string]any{
		"description": "List of containers.",
		"items": map[string]any{
			"description":                "Container describes a single container.",
			keyFluxSchemaTypeDescription: "Container describes a single container.",
		},
	}
	g.Expect(strings.Count(descriptionText(arrayNode), "Container describes a single container.")).To(Equal(1))
}

func TestRendererOpenAPIV2DescriptionSpacing(t *testing.T) {
	g := NewWithT(t)

	primitive := map[string]any{
		"type":                       "object",
		"description":                "Spec configures the pod.",
		keyFluxSchemaType:            "PodSpec",
		keyFluxSchemaTypeDescription: "PodSpec is a description of a pod.",
	}
	root := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": primitive,
		},
	}

	var out bytes.Buffer
	r := renderer{w: &out, format: OutputPlaintextOpenAPIV2}
	g.Expect(r.render(&resolvedSchema{Root: root, Kind: "Pod", Version: "v1"}, []string{"spec"})).To(Succeed())
	g.Expect(out.String()).To(Equal("KIND:     Pod\n" +
		"VERSION:  v1\n" +
		"\n" +
		"FIELD:    spec <PodSpec>\n" +
		"\n" +
		"DESCRIPTION:\n" +
		"     Spec configures the pod.\n" +
		"\n" +
		"     PodSpec is a description of a pod.\n"))

	resource := map[string]any{
		"type":                       "object",
		"description":                "Spec configures the pod.",
		keyFluxSchemaType:            "PodSpec",
		keyFluxSchemaTypeDescription: "PodSpec is a description of a pod.",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Name of the pod."},
		},
	}
	root["properties"].(map[string]any)["spec"] = resource

	out.Reset()
	g.Expect(r.render(&resolvedSchema{Root: root, Kind: "Pod", Version: "v1"}, []string{"spec"})).To(Succeed())
	g.Expect(out.String()).To(Equal("KIND:     Pod\n" +
		"VERSION:  v1\n" +
		"\n" +
		"RESOURCE: spec <PodSpec>\n" +
		"\n" +
		"DESCRIPTION:\n" +
		"     Spec configures the pod.\n" +
		"\n" +
		"     PodSpec is a description of a pod.\n" +
		"\n" +
		"FIELDS:\n" +
		"   name\t<string>\n" +
		"     Name of the pod.\n" +
		"\n"))
}

func TestEcosystemIndexResolveAndComplete(t *testing.T) {
	g := NewWithT(t)

	var helmReleaseRequests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = fmt.Fprint(w, `{"v":3,"projects":[{"groups":[{"g":"helm.toolkit.fluxcd.io","kinds":[["helmrelease",["v2"],1,"HelmRelease",{"n":["hr"]}]]},{"g":"source.extensions.fluxcd.io","kinds":[["artifactgenerator",["v1beta1"],1,"ArtifactGenerator",{"n":["ag"]}]]}]}]}`)
		case "/catalog/helm.toolkit.fluxcd.io/helmrelease_v2.json":
			atomic.AddInt32(&helmReleaseRequests, 1)
			_, _ = fmt.Fprint(w, `{
				"description":"HelmRelease is the Schema for the helmreleases API",
				"properties":{
					"apiVersion":{"type":"string"},
					"kind":{"type":"string"},
					"spec":{"type":"object","description":"HelmReleaseSpec defines the desired state of a Helm release.","properties":{
						"chart":{"type":"object","description":"Chart defines the chart template."},
						"chartRef":{"type":"object","description":"ChartRef defines the chart source."}
					}}
				},
				"type":"object"
			}`)
		case "/catalog/source.extensions.fluxcd.io/artifactgenerator_v1beta1.json":
			_, _ = fmt.Fprint(w, `{
				"description":"ArtifactGenerator is the Schema for the artifactgenerators API",
				"properties":{
					"apiVersion":{"type":"string"},
					"kind":{"type":"string"},
					"spec":{"type":"object","properties":{"pathPattern":{"type":"string","description":"PathPattern specifies a directory traversal pattern."}}}
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

	matches, err = ex.CompleteResourceNames(context.Background(), "ag")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{"artifactgenerators.source.extensions.fluxcd.io"}))

	matches, err = ex.CompleteReferences(context.Background(), "hr.s")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{"hr.spec"}))

	matches, err = ex.CompleteReferences(context.Background(), "hr.spec.ch")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{"hr.spec.chart", "hr.spec.chartRef"}))

	matches, err = ex.CompleteReferences(context.Background(), "helmreleases.helm.toolkit.fluxcd.io.spec.ch")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{
		"helmreleases.helm.toolkit.fluxcd.io.spec.chart",
		"helmreleases.helm.toolkit.fluxcd.io.spec.chartRef",
	}))

	matches, err = ex.CompleteReferences(context.Background(), "ag.spec.p")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(matches).To(Equal([]string{"ag.spec.pathPattern"}))

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
	g.Expect(atomic.LoadInt32(&helmReleaseRequests)).To(Equal(int32(1)))

	out.Reset()
	g.Expect(ex.Explain(context.Background(), "ag.spec.pathPattern", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("GROUP:      source.extensions.fluxcd.io\n"))
	g.Expect(out.String()).To(ContainSubstring("KIND:       ArtifactGenerator\n"))
	g.Expect(out.String()).To(ContainSubstring("FIELD: pathPattern <string>\n"))
}

func TestEcosystemIndexPrefersResourcePriority(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = fmt.Fprint(w, `{"v":3,"projects":[{"groups":[{"g":"events.k8s.io","kinds":[["event",["v1"],1,"Event",{"n":["ev"]}]]},{"g":"core","kinds":[["event",["v1"],1,"Event",{"n":["ev"]}]]}]}]}`)
		case "/catalog/core/event_v1.json":
			_, _ = fmt.Fprint(w, `{
				"description":"Core Event describes a cluster event.",
				"properties":{"metadata":{"type":"object"}},
				"type":"object"
			}`)
		case "/catalog/events.k8s.io/event_v1.json":
			_, _ = fmt.Fprint(w, `{
				"description":"Events API Event describes a cluster event.",
				"properties":{"metadata":{"type":"object"}},
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

	var out bytes.Buffer
	g.Expect(ex.Explain(context.Background(), "events", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("KIND:       Event\n"))
	g.Expect(out.String()).To(ContainSubstring("VERSION:    v1\n"))
	g.Expect(out.String()).To(ContainSubstring("Core Event describes a cluster event."))
	g.Expect(out.String()).ToNot(ContainSubstring("GROUP:"))

	out.Reset()
	g.Expect(ex.Explain(context.Background(), "events.events.k8s.io", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("GROUP:      events.k8s.io\n"))
	g.Expect(out.String()).To(ContainSubstring("Events API Event describes a cluster event."))

	exWithVersion, err := New(Options{
		SchemaLocations: []string{srv.URL + "/catalog/{{.Group}}/{{.Kind}}_{{.Version}}.json"},
		IndexLocations:  []string{srv.URL + "/index.json"},
		APIVersion:      "events.k8s.io/v1",
	})
	g.Expect(err).ToNot(HaveOccurred())

	out.Reset()
	g.Expect(exWithVersion.Explain(context.Background(), "ev", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("GROUP:      events.k8s.io\n"))
	g.Expect(out.String()).To(ContainSubstring("Events API Event describes a cluster event."))
}

func TestEcosystemIndexPrefersKubeAwareVersionPriority(t *testing.T) {
	g := NewWithT(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			_, _ = fmt.Fprint(w, `{"v":3,"projects":[{"groups":[{"g":"example.com","kinds":[["widget",["v1alpha1","v1beta1","v1"],1,"Widget"]]}]}]}`)
		case "/catalog/example.com/widget_v1.json":
			_, _ = fmt.Fprint(w, `{"description":"Widget v1.","properties":{},"type":"object"}`)
		case "/catalog/example.com/widget_v1beta1.json":
			_, _ = fmt.Fprint(w, `{"description":"Widget v1beta1.","properties":{},"type":"object"}`)
		case "/catalog/example.com/widget_v1alpha1.json":
			_, _ = fmt.Fprint(w, `{"description":"Widget v1alpha1.","properties":{},"type":"object"}`)
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

	var out bytes.Buffer
	g.Expect(ex.Explain(context.Background(), "widgets", &out)).To(Succeed())
	g.Expect(out.String()).To(ContainSubstring("GROUP:      example.com\n"))
	g.Expect(out.String()).To(ContainSubstring("VERSION:    v1\n"))
	g.Expect(out.String()).To(ContainSubstring("Widget v1."))
}

func TestCatalogIndexKindDecode(t *testing.T) {
	g := NewWithT(t)

	var kind catalogIndexKind
	g.Expect(kind.UnmarshalJSON([]byte(`["ocirepository",["v1"],1,"OCIRepository",{"n":["ocirepo"]}]`))).To(Succeed())
	g.Expect(kind.Name).To(Equal("ocirepository"))
	g.Expect(kind.Versions).To(Equal([]string{"v1"}))
	g.Expect(kind.Kind).To(Equal("OCIRepository"))
	g.Expect(kind.Resource.ShortNames).To(Equal([]string{"ocirepo"}))
}
