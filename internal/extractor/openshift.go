// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"regexp"
	"strings"
)

// openShiftDefinitionPrefix is the prefix every openshift/api swagger
// definition key shares. Keys outside this namespace (the upstream
// Kubernetes types embedded in the same document) are inlined when
// referenced but never emitted as standalone Schemas.
const openShiftDefinitionPrefix = "com.github.openshift.api."

// openShiftDirToGroup maps the swagger directory segment (the token after
// com.github.openshift.api.) to the canonical Kubernetes API group declared
// in openshift/api's <dir>/v*/register.go GroupName constants. Verified at
// release-4.20 against every dir present in the swagger.
//
// legacyconfig is intentionally absent: its LegacyGroupName is "" (config-
// file format, not an API group) and every legacyconfig definition fails
// the GitOps-scope filter regardless.
var openShiftDirToGroup = map[string]string{
	"apiserver":             "apiserver.openshift.io",
	"apps":                  "apps.openshift.io",
	"authorization":         "authorization.openshift.io",
	"build":                 "build.openshift.io",
	"cloudnetwork":          "cloud.network.openshift.io",
	"config":                "config.openshift.io",
	"console":               "console.openshift.io",
	"example":               "example.openshift.io",
	"helm":                  "helm.openshift.io",
	"image":                 "image.openshift.io",
	"insights":              "insights.openshift.io",
	"kubecontrolplane":      "kubecontrolplane.config.openshift.io",
	"machine":               "machine.openshift.io",
	"machineconfiguration":  "machineconfiguration.openshift.io",
	"monitoring":            "monitoring.openshift.io",
	"network":               "network.openshift.io",
	"networkoperator":       "network.operator.openshift.io",
	"oauth":                 "oauth.openshift.io",
	"openshiftcontrolplane": "openshiftcontrolplane.config.openshift.io",
	"operator":              "operator.openshift.io",
	"operatorcontrolplane":  "controlplane.operator.openshift.io",
	"operatoringress":       "ingress.operator.openshift.io",
	"osin":                  "osin.config.openshift.io",
	"project":               "project.openshift.io",
	"quota":                 "quota.openshift.io",
	"route":                 "route.openshift.io",
	"samples":               "samples.operator.openshift.io",
	"security":              "security.openshift.io",
	"securityinternal":      "security.internal.openshift.io",
	"servicecertsigner":     "servicecertsigner.config.openshift.io",
	"sharedresource":        "sharedresource.openshift.io",
	"template":              "template.openshift.io",
	"user":                  "user.openshift.io",
}

var (
	// openShiftVersionRe accepts the K8s version forms used by openshift/api:
	// stable (vN), alpha (vNalphaN), beta (vNbetaN).
	openShiftVersionRe = regexp.MustCompile(`^v\d+(alpha\d+|beta\d+)?$`)

	// openShiftSkipKindRe matches kind names that are not GitOps-applicable.
	openShiftSkipKindRe = regexp.MustCompile(`^[A-Za-z]+(ReviewResponse|Review|ProviderConfig|ProviderSpec|ProviderStatus|Options)$`)
)

// ExtractOpenShift walks an openshift/api OpenAPI v2 swagger document and
// returns one Schema per top-level OpenShift resource, with all $refs
// inlined (including refs to upstream Kubernetes types embedded in the same
// document) and the standalone-strict transforms applied. Definitions
// outside the com.github.openshift.api.* namespace are inlined when
// referenced but never emitted as their own Schema, so the catalog can
// never accidentally overwrite a Kubernetes schema with an OpenShift copy.
//
// The returned slice is sorted by (Group, Version, Kind). Errors are
// aggregated: a malformed definition does not stop extraction of the rest.
func ExtractOpenShift(data []byte) ([]Schema, []error) {
	root, definitions, names, errs := parseSwaggerDocument(data)
	if errs != nil {
		return nil, errs
	}
	scopes := scopeFromPaths(root)
	source := sourceWithVersion("OpenShift", openShiftInfoVersion(root))

	var out []Schema
	for _, name := range names {
		def, ok := definitions[name].(map[string]any)
		if !ok {
			continue
		}
		// Namespace filter: only OpenShift definitions are candidates.
		if !strings.HasPrefix(name, openShiftDefinitionPrefix) {
			continue
		}
		// GitOps-scope filter: the kind must look like a persistable resource.
		if !hasGitOpsShape(def) {
			continue
		}
		// GVK derivation must precede the suffix filter so the regex applies
		// to just the <Kind> segment, not the dotted definition key.
		gvk, err := deriveOpenShiftGVK(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// Suffix filter: drop list/review/options/provider* shapes that are
		// never persisted, even though they pass the structural filter.
		if openShiftSkipKindRe.MatchString(gvk.Kind) {
			continue
		}
		// Catalog-safety guard: a bug in the dir map or the namespace filter
		// must never produce a non-OpenShift group.
		if !strings.HasSuffix(gvk.Group, ".openshift.io") {
			errs = append(errs, fmt.Errorf("internal: derived group %q for %q is not under .openshift.io", gvk.Group, name))
			continue
		}
		schema, err := buildSchema(name, def, definitions)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		out = append(out, Schema{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
			Scope:   scopes[gvk],
			Source:  source,
			JSON:    schema,
		})
	}

	sortSchemasByGVK(out)
	return out, errs
}

func openShiftInfoVersion(root map[string]any) string {
	info, _ := root["info"].(map[string]any)
	version, _ := info["version"].(string)
	return version
}

// hasGitOpsShape reports whether def carries the apiVersion, kind, and
// metadata properties that distinguish a GitOps-applicable resource from
// a helper or subresource type. Subresource request payloads
// (BuildLog, DeploymentRequest, …) lack metadata and are filtered here.
func hasGitOpsShape(def map[string]any) bool {
	props, ok := def["properties"].(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"apiVersion", "kind", "metadata"} {
		if _, has := props[key]; !has {
			return false
		}
	}
	return true
}

// deriveOpenShiftGVK parses an OpenShift definition key into a GVK. Keys
// are shaped com.github.openshift.api.<dir>.<version>.<Kind>. The dir is
// looked up in openShiftDirToGroup; an unknown dir is a hard error so a
// future openshift/api directory addition surfaces as a generator failure
// instead of silently producing a wrong group.
func deriveOpenShiftGVK(name string) (GVK, error) {
	suffix := strings.TrimPrefix(name, openShiftDefinitionPrefix)
	parts := strings.SplitN(suffix, ".", 3)
	if len(parts) != 3 {
		return GVK{}, fmt.Errorf("openshift definition %q is malformed (expected <dir>.<version>.<Kind>)", name)
	}
	dir, version, kind := parts[0], parts[1], parts[2]
	if !openShiftVersionRe.MatchString(version) {
		return GVK{}, fmt.Errorf("openshift definition %q has unsupported version %q", name, version)
	}
	if kind == "" {
		return GVK{}, fmt.Errorf("openshift definition %q has empty kind segment", name)
	}
	// SplitN consumes only the first two dots, so a synthetic key like
	// com.github.openshift.api.<dir>.v1.Foo.Bar would yield kind="Foo.Bar"
	// and produce a file path with an embedded dot. No such kind exists
	// today, but we reject it here to keep the parse strict.
	if strings.Contains(kind, ".") {
		return GVK{}, fmt.Errorf("openshift definition %q has dotted kind segment %q", name, kind)
	}
	group, ok := openShiftDirToGroup[dir]
	if !ok {
		return GVK{}, fmt.Errorf("openshift definition %q uses unknown dir %q (add it to openShiftDirToGroup)", name, dir)
	}
	return GVK{Group: group, Version: version, Kind: kind}, nil
}
