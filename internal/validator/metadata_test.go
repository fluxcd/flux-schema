// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestValidateMetadata_Valid(t *testing.T) {
	g := NewWithT(t)
	doc := map[string]any{
		"metadata": map[string]any{
			"name":      "valid-name",
			"namespace": "default",
			"labels": map[string]any{
				"app.kubernetes.io/name": "widget",
			},
			"annotations": map[string]any{
				"example.com/note": "hello",
			},
		},
	}
	g.Expect(validateMetadata(doc)).To(BeEmpty())
}

func TestValidateMetadata_NoMetadata(t *testing.T) {
	g := NewWithT(t)
	g.Expect(validateMetadata(map[string]any{})).To(BeEmpty())
}

func TestValidateMetadata_BadName(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{"name": "Bad_Name"},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/name"))
}

func TestValidateMetadata_BadGenerateName(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{"generateName": "Bad_Prefix-"},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/generateName"))
}

func TestValidateMetadata_RBACAllowsUnderscoreInName(t *testing.T) {
	g := NewWithT(t)
	for _, kind := range []string{"Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding"} {
		errs := validateMetadata(map[string]any{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       kind,
			"metadata":   map[string]any{"name": "ops_clusterRole2"},
		})
		g.Expect(errs).To(BeEmpty(), "kind %s should accept underscored name", kind)
	}
}

func TestValidateMetadata_RBACRejectsPathSegmentViolation(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": "bad/name"},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/name"))
}

func TestValidateMetadata_NonRBACKindWithRoleNameStillDNSValidated(t *testing.T) {
	g := NewWithT(t)
	// A CRD that happens to be named "Role" in a different group must still
	// get DNS-subdomain validation.
	errs := validateMetadata(map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Role",
		"metadata":   map[string]any{"name": "ops_clusterRole2"},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/name"))
}

func TestValidateMetadata_BadNamespace(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{"namespace": "Not.Valid"},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/namespace"))
}

func TestValidateMetadata_BadLabelKey(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"bad key!": "value"},
		},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/labels/bad key!"))
	g.Expect(errs[0].Msg).To(HavePrefix("key:"))
}

func TestValidateMetadata_BadLabelValue(t *testing.T) {
	g := NewWithT(t)
	long := strings.Repeat("a", 64) // label values cap at 63 chars
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"app": long},
		},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/labels/app"))
}

func TestValidateMetadata_LabelKeyJSONPointerEscape(t *testing.T) {
	g := NewWithT(t)
	// A long value forces a value violation so we can assert the encoded path.
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"example.com/foo": strings.Repeat("a", 64)},
		},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/labels/example.com~1foo"))
}

func TestValidateMetadata_NonStringLabelValue(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"app": 42},
		},
	})
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Path).To(Equal("/metadata/labels/app"))
	g.Expect(errs[0].Msg).To(Equal("must be a string, got number"))
}

func TestValidateMetadata_NonStringLabelValueMapping(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{"app": map[string]any{"nested": "x"}},
		},
	})
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Msg).To(Equal("must be a string, got mapping"))
}

func TestValidateMetadata_BadAnnotationKey(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{"bad key!": "v"},
		},
	})
	g.Expect(errs).ToNot(BeEmpty())
	g.Expect(errs[0].Path).To(Equal("/metadata/annotations/bad key!"))
	g.Expect(errs[0].Msg).To(HavePrefix("key:"))
}

func TestValidateMetadata_AnnotationsTotalSizeExceeded(t *testing.T) {
	g := NewWithT(t)
	// Two values that together cross the 256 KiB total-size cap.
	big := strings.Repeat("a", 200*1024)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"a": big,
				"b": big,
			},
		},
	})
	found := false
	for _, e := range errs {
		if e.Path == "/metadata/annotations" && strings.Contains(e.Msg, "total size") {
			found = true
		}
	}
	g.Expect(found).To(BeTrue(), "expected total-size error at /metadata/annotations, got %+v", errs)
}

func TestValidateMetadata_NonStringAnnotationValue(t *testing.T) {
	g := NewWithT(t)
	errs := validateMetadata(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{"k": 1},
		},
	})
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Path).To(Equal("/metadata/annotations/k"))
}

func TestValidateBytes_MetadataViolation(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: Bad_Name
spec:
  name: w1
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	g.Expect(results[0].Reason).To(Equal(ReasonSchemaViolation))
	paths := make(map[string]bool)
	for _, e := range results[0].Errors {
		paths[e.Path] = true
	}
	g.Expect(paths).To(HaveKey("/metadata/name"))
}

// TestValidateBytes_SchemaAndMetadataViolations pins that both error sets
// surface in one Result so users don't have to re-run after fixing one half.
func TestValidateBytes_SchemaAndMetadataViolations(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	writeWidgetSchema(t, dir)
	v := newLocalValidator(t, dir, false)

	doc := []byte(`apiVersion: example.com/v1
kind: Widget
metadata:
  name: Bad_Name
spec:
  name: 42
`)
	results := v.ValidateBytes(context.Background(), "test.yaml", doc)
	g.Expect(results).To(HaveLen(1))
	g.Expect(results[0].Status).To(Equal(StatusInvalid))
	paths := make(map[string]bool)
	for _, e := range results[0].Errors {
		paths[e.Path] = true
	}
	g.Expect(paths).To(HaveKey("/metadata/name"))
	g.Expect(paths).To(HaveKey("/spec/name"))
}

func TestShortenRule(t *testing.T) {
	g := NewWithT(t)
	g.Expect(shortenRule(
		"a valid label must consist of (e.g. 'X', regex used for validation is '[a-z]+')",
	)).To(Equal("must match regex '[a-z]+'"))
	g.Expect(shortenRule("must not contain dots")).To(Equal("must not contain dots"))
	g.Expect(shortenRule("must be no more than 63 characters")).To(Equal("must be no more than 63 characters"))
}

func TestEscapeJSONPointer(t *testing.T) {
	g := NewWithT(t)
	g.Expect(escapeJSONPointer("plain")).To(Equal("plain"))
	g.Expect(escapeJSONPointer("a/b")).To(Equal("a~1b"))
	g.Expect(escapeJSONPointer("a~b")).To(Equal("a~0b"))
	// '~' must be encoded before '/' so '~1' inputs aren't double-escaped.
	g.Expect(escapeJSONPointer("~1")).To(Equal("~01"))
}
