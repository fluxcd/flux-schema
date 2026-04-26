// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"maps"
	"testing"

	. "github.com/onsi/gomega"
)

// gitopsKindDef returns a definition that passes the GitOps-scope filter:
// it carries apiVersion, kind, and metadata properties. Callers can
// override or extend properties via the with map.
func gitopsKindDef(with map[string]any) map[string]any {
	props := map[string]any{
		"apiVersion": map[string]any{"type": "string"},
		"kind":       map[string]any{"type": "string"},
		"metadata":   map[string]any{"type": "object"},
		"spec":       map[string]any{"type": "object"},
	}
	maps.Copy(props, with)
	return map[string]any{
		"type":       "object",
		"properties": props,
	}
}

func swaggerWith(t *testing.T, defs map[string]any) []byte {
	t.Helper()
	return mustMarshal(t, map[string]any{
		"swagger":     "2.0",
		"definitions": defs,
	})
}

func TestExtractOpenShift_NoDefinitions(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractOpenShift([]byte(`{}`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("no 'definitions'"))
}

func TestExtractOpenShift_InvalidJSON(t *testing.T) {
	g := NewWithT(t)
	_, errs := ExtractOpenShift([]byte(`not json`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("decode swagger"))
}

func TestExtractOpenShift_RouteHappyPath(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.v1.Route": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Group).To(Equal("route.openshift.io"))
	g.Expect(schemas[0].Version).To(Equal("v1"))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
	required := schemas[0].JSON["required"].([]any)
	g.Expect(required).To(ContainElement("apiVersion"))
	g.Expect(required).To(ContainElement("kind"))
	g.Expect(schemas[0].JSON["additionalProperties"]).To(BeFalse())
	// $schema is intentionally not injected — the catalog's K8s schemas
	// also omit it, and a stray injection here would diverge.
	_, hasSchema := schemas[0].JSON["$schema"]
	g.Expect(hasSchema).To(BeFalse())
}

func TestExtractOpenShift_AlphaVersion(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.config.v1alpha1.Foo": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Version).To(Equal("v1alpha1"))
	g.Expect(schemas[0].Kind).To(Equal("Foo"))
}

func TestExtractOpenShift_CompoundGroup(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.cloudnetwork.v1.CloudPrivateIPConfig": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Group).To(Equal("cloud.network.openshift.io"))
}

func TestExtractOpenShift_OperatorIngressGroup(t *testing.T) {
	// operatoringress is one of the compound-group cases where the dir
	// segment is *not* the prefix of the group — guards against a naive
	// `<dir>.openshift.io` fallback.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.operatoringress.v1.DNSRecord": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Group).To(Equal("ingress.operator.openshift.io"))
}

func TestExtractOpenShift_HelperTypeNotEmitted(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.v1.RouteSpec": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{"type": "string"},
			},
		},
		"com.github.openshift.api.route.v1.Route": gitopsKindDef(map[string]any{
			"spec": map[string]any{"$ref": "#/definitions/com.github.openshift.api.route.v1.RouteSpec"},
		}),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
	// The RouteSpec ref was inlined into Route.spec.
	spec := schemas[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	g.Expect(specProps).To(HaveKey("host"))
}

func TestExtractOpenShift_K8sDefinitionsNotEmitted(t *testing.T) {
	// Catalog-safety invariant: K8s types embedded in the OpenShift
	// swagger must not be emitted, otherwise this run would overwrite
	// entries produced by gen-k8s-schemas.sh.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"io.k8s.api.core.v1.Pod":                  gitopsKindDef(nil),
		"com.github.openshift.api.route.v1.Route": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
}

func TestExtractOpenShift_RefToK8sTypeInlined(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
		"com.github.openshift.api.route.v1.Route": gitopsKindDef(map[string]any{
			"metadata": map[string]any{"$ref": "#/definitions/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"},
		}),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	meta := schemas[0].JSON["properties"].(map[string]any)["metadata"].(map[string]any)
	metaProps := meta["properties"].(map[string]any)
	g.Expect(metaProps).To(HaveKey("name"))
}

func TestExtractOpenShift_ListsEmitted(t *testing.T) {
	// *List kinds are applyable Git manifests; the K8s catalog ships
	// every *List schema, and OpenShift mirrors that for parity.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.v1.Route":     gitopsKindDef(nil),
		"com.github.openshift.api.route.v1.RouteList": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(2))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
	g.Expect(schemas[1].Kind).To(Equal("RouteList"))
}

func TestExtractOpenShift_SuffixReviewSkipped(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.authorization.v1.SubjectAccessReview":         gitopsKindDef(nil),
		"com.github.openshift.api.authorization.v1.SubjectAccessReviewResponse": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(BeEmpty())
}

func TestExtractOpenShift_SuffixProviderConfigSkipped(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.machine.v1beta1.AWSMachineProviderConfig": gitopsKindDef(nil),
		"com.github.openshift.api.machine.v1beta1.AWSMachineProviderSpec":   gitopsKindDef(nil),
		"com.github.openshift.api.machine.v1beta1.AWSMachineProviderStatus": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(BeEmpty())
}

func TestExtractOpenShift_SuffixOptionsSkipped(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.build.v1.BinaryBuildRequestOptions": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(BeEmpty())
}

func TestExtractOpenShift_SuffixRegexDoesNotOverMatch(t *testing.T) {
	// Console / Routelistener / Configuration share substrings with the
	// excluded suffixes but must stay in the output — guards against the
	// regex losing its anchor or its case-sensitivity.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.console.v1.Console":      gitopsKindDef(nil),
		"com.github.openshift.api.route.v1.Routelistener":  gitopsKindDef(nil),
		"com.github.openshift.api.config.v1.Configuration": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(3))
	kinds := []string{schemas[0].Kind, schemas[1].Kind, schemas[2].Kind}
	g.Expect(kinds).To(ConsistOf("Console", "Configuration", "Routelistener"))
}

func TestExtractOpenShift_SubresourceWithoutMetadataSkipped(t *testing.T) {
	// BuildLog and DeploymentRequest carry apiVersion+kind but no
	// metadata — they are POST/GET payloads on subresource endpoints
	// (/log, /instantiate) and are never written to Git. The structural
	// filter excludes them silently.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.build.v1.BuildLog": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"apiVersion": map[string]any{"type": "string"},
				"kind":       map[string]any{"type": "string"},
			},
		},
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(BeEmpty())
}

func TestExtractOpenShift_LegacyConfigSkipped(t *testing.T) {
	// legacyconfig is intentionally absent from openShiftDirToGroup
	// (LegacyGroupName = "" upstream). The structural filter already drops
	// every legacyconfig definition at release-4.20; this test pins the
	// belt-and-suspenders behavior for a hypothetical future definition
	// that grew apiVersion+kind+metadata properties.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.legacyconfig.v1.MasterConfig": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(BeEmpty())
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("legacyconfig"))
}

func TestExtractOpenShift_UnknownDirError(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.brandnewthing.v1.Foo": gitopsKindDef(nil),
		"com.github.openshift.api.route.v1.Route":       gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("brandnewthing"))
}

func TestExtractOpenShift_MalformedKey(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.notakind":       gitopsKindDef(nil),
		"com.github.openshift.api.route.v1.Route": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(HaveLen(1))
	g.Expect(schemas[0].Kind).To(Equal("Route"))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("malformed"))
}

func TestExtractOpenShift_DottedKindRejected(t *testing.T) {
	// SplitN with limit=3 consumes only the first two dots, so a key with
	// a trailing dot in the kind segment would slip past the structural
	// parse. The explicit no-dot check rejects it.
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.v1.Foo.Bar": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(BeEmpty())
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("dotted kind"))
}

func TestExtractOpenShift_BadVersionSegment(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.gamma.Route": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(BeEmpty())
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("unsupported version"))
}

func TestExtractOpenShift_CatalogSafetyGuard(t *testing.T) {
	// Inject a non-OpenShift group into the dir map to prove the suffix
	// guard fires before any Schema is emitted with a wrong group.
	// Restored via t.Cleanup so other tests are unaffected.
	//
	// This test mutates a package-level map. Do NOT add t.Parallel() here
	// or to any sibling test in this package — concurrent reads from
	// deriveOpenShiftGVK would race with the write below.
	g := NewWithT(t)
	openShiftDirToGroup["badtest"] = "badtest.example.com"
	t.Cleanup(func() { delete(openShiftDirToGroup, "badtest") })

	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.badtest.v1.Foo": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(schemas).To(BeEmpty())
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("not under .openshift.io"))
}

func TestExtractOpenShift_DeterministicSort(t *testing.T) {
	g := NewWithT(t)
	data := swaggerWith(t, map[string]any{
		"com.github.openshift.api.route.v1.Route":           gitopsKindDef(nil),
		"com.github.openshift.api.config.v1.Build":          gitopsKindDef(nil),
		"com.github.openshift.api.config.v1alpha1.Build":    gitopsKindDef(nil),
		"com.github.openshift.api.apps.v1.DeploymentConfig": gitopsKindDef(nil),
	})
	schemas, errs := ExtractOpenShift(data)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(4))
	// Sorted by (Group, Version, Kind).
	g.Expect(schemas[0].Group).To(Equal("apps.openshift.io"))
	g.Expect(schemas[1].Group).To(Equal("config.openshift.io"))
	g.Expect(schemas[1].Version).To(Equal("v1"))
	g.Expect(schemas[2].Group).To(Equal("config.openshift.io"))
	g.Expect(schemas[2].Version).To(Equal("v1alpha1"))
	g.Expect(schemas[3].Group).To(Equal("route.openshift.io"))
}
