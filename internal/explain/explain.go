// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package explain renders kubectl-style schema explanations from flux-schema
// catalog JSON Schemas.
package explain

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeversion "k8s.io/apimachinery/pkg/version"

	"github.com/fluxcd/flux-schema/internal/tmpl"
)

const (
	OutputPlaintext          = "plaintext"
	OutputPlaintextOpenAPIV2 = "plaintext-openapiv2"

	// MetadataDir is the catalog root subdirectory for explain lookup metadata.
	MetadataDir = ".explain"

	// ReferencesDir contains exact resource-reference lookup files.
	ReferencesDir = "refs"

	// CompletionDir contains resource-reference completion shard files.
	CompletionDir = "completion"

	keyProperties = "properties"
	keyType       = "type"
	keyItems      = "items"
	keyRequired   = "required"
	keyEnum       = "enum"
	keyDesc       = "description"
	keyAddlProps  = "additionalProperties"
	keyAllOf      = "allOf"

	keyFluxSchemaType            = "x-flux-schema-type"
	keyFluxSchemaTypeDescription = "x-flux-schema-type-description"
	keyFluxSchemaGVK             = "x-flux-schema-group-version-kind"
	keyFluxSchemaAlias           = "x-flux-schema-alias"

	maxAliasDepth = 8
	typeObject    = "Object"
)

var kubernetesGroups = []string{
	"",
	"apps",
	"batch",
	"autoscaling",
	"policy",
	"networking.k8s.io",
	"rbac.authorization.k8s.io",
	"apiextensions.k8s.io",
	"apiregistration.k8s.io",
	"admissionregistration.k8s.io",
	"storage.k8s.io",
	"coordination.k8s.io",
	"scheduling.k8s.io",
	"node.k8s.io",
	"discovery.k8s.io",
	"events.k8s.io",
	"certificates.k8s.io",
	"authentication.k8s.io",
	"authorization.k8s.io",
	"resource.k8s.io",
	"flowcontrol.apiserver.k8s.io",
}

var versionCandidates = []string{
	"v1",
	"v2",
	"v1beta1",
	"v1beta2",
	"v1beta3",
	"v1alpha1",
	"v1alpha2",
	"v1alpha3",
	"v2beta1",
	"v2alpha1",
}

type Options struct {
	SchemaLocations       []string
	MetadataLocations     []string
	IndexLocations        []string
	APIVersion            string
	OutputFormat          string
	Recursive             bool
	HTTPClient            *retryablehttp.Client
	HTTPTimeout           time.Duration
	InsecureSkipTLSVerify bool
}

type Explainer struct {
	opts      Options
	templates []*template.Template
	client    *retryablehttp.Client

	indexLoaded    bool
	indexResources []indexResource
	indexErr       error
}

type resolvedSchema struct {
	Root          map[string]any
	Location      string
	Group         string
	Version       string
	Kind          string
	RequestedKind string
}

type requestCandidate struct {
	resource string
	group    string
	fields   []string
}

type fieldIndexMetadata struct {
	APIVersion string
	Kind       string
}

type resourceReference struct {
	APIVersion string           `json:"apiVersion,omitempty"`
	Kind       string           `json:"kind,omitempty"`
	Targets    []resourceTarget `json:"targets,omitempty"`
}

type resourceTarget struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

type completionShard struct {
	APIVersion string               `json:"apiVersion,omitempty"`
	Kind       string               `json:"kind,omitempty"`
	Resources  []completionResource `json:"resources,omitempty"`
}

type completionResource struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

type catalogIndex struct {
	Projects []catalogIndexProject `json:"projects,omitempty"`
}

type catalogIndexProject struct {
	Groups []catalogIndexGroup `json:"groups,omitempty"`
}

type catalogIndexGroup struct {
	Group string             `json:"g,omitempty"`
	Kinds []catalogIndexKind `json:"kinds,omitempty"`
}

type catalogIndexKind struct {
	Name     string
	Versions []string
	Kind     string
	Resource catalogIndexResource
}

type catalogIndexResource struct {
	Singular   string   `json:"s,omitempty"`
	Plural     string   `json:"p,omitempty"`
	ShortNames []string `json:"n,omitempty"`
}

type indexResource struct {
	Group      string
	Version    string
	Kind       string
	Name       string
	Singular   string
	Plural     string
	ShortNames []string
	order      int
}

func (k *catalogIndexKind) UnmarshalJSON(data []byte) error {
	var tuple []json.RawMessage
	if err := json.Unmarshal(data, &tuple); err != nil {
		return err
	}
	if len(tuple) < 2 {
		return fmt.Errorf("index kind tuple has %d entries, want at least 2", len(tuple))
	}
	if err := json.Unmarshal(tuple[0], &k.Name); err != nil {
		return fmt.Errorf("parse index kind name: %w", err)
	}
	if err := json.Unmarshal(tuple[1], &k.Versions); err != nil {
		return fmt.Errorf("parse index kind versions: %w", err)
	}
	if len(tuple) >= 4 && string(tuple[3]) != "null" {
		if err := json.Unmarshal(tuple[3], &k.Kind); err != nil {
			return fmt.Errorf("parse index kind display name: %w", err)
		}
	}
	if len(tuple) >= 5 {
		if err := json.Unmarshal(tuple[4], &k.Resource); err != nil {
			return fmt.Errorf("parse index kind resource names: %w", err)
		}
	}
	if k.Kind == "" {
		k.Kind = fallbackKind(k.Name)
	}
	return nil
}

func New(opts Options) (*Explainer, error) {
	locations := opts.SchemaLocations
	if len(locations) == 0 {
		return nil, errors.New("at least one schema location is required")
	}
	compiled := make([]*template.Template, 0, len(locations))
	for _, loc := range locations {
		tpl, err := tmpl.Parse(loc)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, tpl)
	}
	client := opts.HTTPClient
	if client == nil {
		client = retryablehttp.NewClient()
		client.Logger = nil
		if opts.InsecureSkipTLSVerify {
			client.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		}
	}
	opts.SchemaLocations = locations
	return &Explainer{opts: opts, templates: compiled, client: client}, nil
}

func (e *Explainer) Explain(ctx context.Context, resourceExpr string, w io.Writer) error {
	if e.opts.OutputFormat != "" && e.opts.OutputFormat != OutputPlaintext && e.opts.OutputFormat != OutputPlaintextOpenAPIV2 {
		return fmt.Errorf("unrecognized format: %s", e.opts.OutputFormat)
	}
	gv, hasGV, err := parseAPIVersion(e.opts.APIVersion)
	if err != nil {
		return err
	}
	candidates, err := parseResourceExpression(resourceExpr, hasGV)
	if err != nil {
		return err
	}
	resolved, fields, err := e.resolve(ctx, candidates, gv, hasGV)
	if err != nil {
		return err
	}
	format := e.opts.OutputFormat
	if format == "" {
		format = OutputPlaintext
	}
	r := renderer{w: w, recursive: e.opts.Recursive, format: format}
	return r.render(resolved, fields)
}

// CompleteResourceNames returns canonical resource references matching prefix.
// Grouped resources are returned as plural.group; core resources as plural.
func (e *Explainer) CompleteResourceNames(ctx context.Context, prefix string) ([]string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	seen := map[string]bool{}
	var out []string
	for _, key := range completionShardKeys(prefix) {
		resources, err := e.loadCompletionShard(ctx, key)
		if err != nil {
			return nil, err
		}
		for _, resource := range resources {
			if !resource.matchesCompletionPrefix(prefix) || seen[resource.Name] {
				continue
			}
			seen[resource.Name] = true
			out = append(out, resource.Name)
		}
	}
	indexResources, err := e.loadResourceIndex(ctx)
	if err != nil {
		return nil, err
	}
	for _, resource := range indexResources {
		name := resource.canonicalName()
		if !resource.matchesCompletionPrefix(prefix) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func parseAPIVersion(apiVersion string) (schema.GroupVersion, bool, error) {
	if apiVersion == "" {
		return schema.GroupVersion{}, false, nil
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersion{}, false, err
	}
	return gv, true, nil
}

func parseResourceExpression(expr string, apiVersionSet bool) ([]requestCandidate, error) {
	expr = strings.TrimSuffix(strings.TrimSpace(expr), ".")
	if expr == "" {
		return nil, errors.New("resource type must not be empty")
	}
	parts := strings.Split(expr, ".")
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid jsonpath syntax, all nodes must be field nodes")
		}
	}
	if apiVersionSet || len(parts) == 1 {
		return []requestCandidate{{resource: parts[0], fields: parts[1:]}}, nil
	}
	out := make([]requestCandidate, 0, len(parts))
	for n := len(parts); n >= 2; n-- {
		group := strings.Join(parts[1:n], ".")
		if !looksLikeAPIGroup(group) {
			continue
		}
		out = append(out, requestCandidate{resource: parts[0], group: group, fields: parts[n:]})
	}
	out = append(out, requestCandidate{resource: parts[0], fields: parts[1:]})
	return out, nil
}

func looksLikeAPIGroup(group string) bool {
	return strings.Contains(group, ".") || slices.Contains(kubernetesGroups, group)
}

func (e *Explainer) resolve(ctx context.Context, requests []requestCandidate, gv schema.GroupVersion, hasGV bool) (*resolvedSchema, []string, error) {
	var attempted []string
	for _, req := range requests {
		resolved, found, err := e.resolveFromReferences(ctx, req, gv, hasGV)
		if err != nil {
			return nil, nil, err
		}
		if found {
			return resolved, req.fields, nil
		}

		groups := []string{req.group}
		if hasGV {
			groups = []string{gv.Group}
		} else if req.group == "" {
			groups = kubernetesGroups
		}
		versions := versionCandidates
		if hasGV {
			versions = []string{gv.Version}
		}
		kinds := kindNameCandidates(req.resource)
		for _, group := range groups {
			for _, version := range versions {
				for _, kind := range kinds {
					resolved, found, err := e.loadGVK(ctx, group, version, kind)
					if err != nil {
						return nil, nil, err
					}
					attempted = append(attempted, schema.GroupVersionKind{Group: group, Version: version, Kind: kind}.String())
					if !found {
						continue
					}
					resolved.RequestedKind = kind
					return resolved, req.fields, nil
				}
			}
		}
	}
	if len(attempted) > 0 {
		return nil, nil, fmt.Errorf("couldn't find resource for %q", attempted[0])
	}
	return nil, nil, errors.New("couldn't find resource")
}

func (e *Explainer) resolveFromReferences(ctx context.Context, req requestCandidate, gv schema.GroupVersion, hasGV bool) (*resolvedSchema, bool, error) {
	refs, err := e.loadResourceReference(ctx, referenceKey(req.resource, req.group, hasGV))
	if err != nil || len(refs) == 0 {
		return nil, false, err
	}
	for _, target := range refs {
		if !target.matchesGroupVersion(req.group, gv, hasGV) {
			continue
		}
		resolved, found, err := e.loadGVK(ctx, target.Group, target.Version, target.Kind)
		if err != nil {
			return nil, false, err
		}
		if !found {
			continue
		}
		if target.Kind != "" {
			resolved.Kind = target.Kind
		}
		resolved.RequestedKind = req.resource
		return resolved, true, nil
	}
	return nil, false, nil
}

func referenceKey(resource, group string, apiVersionSet bool) string {
	resource = strings.ToLower(strings.TrimSpace(resource))
	group = strings.ToLower(strings.TrimSpace(group))
	if resource == "" || group == "" || apiVersionSet {
		return resource
	}
	return resource + "." + group
}

func (e *Explainer) loadResourceReference(ctx context.Context, key string) ([]resourceTarget, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil, nil
	}
	var out []resourceTarget
	for _, root := range e.opts.MetadataLocations {
		location := metadataLocation(root, ReferencesDir, key+".json")
		body, found, err := e.loadBytes(ctx, location)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		var ref resourceReference
		if err := decodeJSON(body, &ref); err != nil {
			return nil, fmt.Errorf("%s: parse explain reference: %w", location, err)
		}
		out = append(out, ref.Targets...)
	}
	indexResources, err := e.loadResourceIndex(ctx)
	if err != nil {
		return nil, err
	}
	for _, resource := range indexResources {
		if resource.matchesReferenceKey(key) {
			out = append(out, resource.target())
		}
	}
	return out, nil
}

func (e *Explainer) loadCompletionShard(ctx context.Context, key string) ([]completionResource, error) {
	var out []completionResource
	for _, root := range e.opts.MetadataLocations {
		location := metadataLocation(root, CompletionDir, key+".json")
		body, found, err := e.loadBytes(ctx, location)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		var shard completionShard
		if err := decodeJSON(body, &shard); err != nil {
			return nil, fmt.Errorf("%s: parse explain completion shard: %w", location, err)
		}
		out = append(out, shard.Resources...)
	}
	return out, nil
}

func (e *Explainer) loadResourceIndex(ctx context.Context) ([]indexResource, error) {
	if e.indexLoaded {
		return e.indexResources, e.indexErr
	}
	e.indexLoaded = true
	for _, location := range e.opts.IndexLocations {
		body, found, err := e.loadBytes(ctx, location)
		if err != nil {
			e.indexErr = err
			return nil, err
		}
		if !found {
			continue
		}
		var index catalogIndex
		if err := decodeJSON(body, &index); err != nil {
			e.indexErr = fmt.Errorf("%s: parse catalog index: %w", location, err)
			return nil, e.indexErr
		}
		resources := index.resources()
		for i := range resources {
			resources[i].order = len(e.indexResources) + i
		}
		e.indexResources = append(e.indexResources, resources...)
	}
	sortIndexResources(e.indexResources)
	return e.indexResources, nil
}

func (i catalogIndex) resources() []indexResource {
	var out []indexResource
	for _, project := range i.Projects {
		for _, group := range project.Groups {
			for _, kind := range group.Kinds {
				name := strings.ToLower(strings.TrimSpace(kind.Name))
				if name == "" {
					continue
				}
				for _, version := range kind.Versions {
					version = strings.TrimSpace(version)
					if version == "" {
						continue
					}
					out = append(out, indexResource{
						Group:      normalizeIndexGroup(group.Group),
						Version:    version,
						Kind:       kind.Kind,
						Name:       name,
						Singular:   strings.ToLower(strings.TrimSpace(kind.Resource.Singular)),
						Plural:     strings.ToLower(strings.TrimSpace(kind.Resource.Plural)),
						ShortNames: normalizedNames(kind.Resource.ShortNames),
					})
				}
			}
		}
	}
	return out
}

func (r indexResource) target() resourceTarget {
	return resourceTarget{Group: r.Group, Version: r.Version, Kind: r.Kind}
}

func (r indexResource) canonicalName() string {
	name := r.pluralName()
	if r.Group == "" {
		return name
	}
	return name + "." + r.Group
}

func (r indexResource) matchesReferenceKey(key string) bool {
	for _, alias := range r.aliases() {
		if key == alias {
			return true
		}
	}
	return false
}

func (r indexResource) matchesCompletionPrefix(prefix string) bool {
	if prefix == "" || strings.HasPrefix(r.canonicalName(), prefix) {
		return true
	}
	for _, alias := range r.aliases() {
		if strings.HasPrefix(alias, prefix) {
			return true
		}
	}
	return false
}

func (r indexResource) aliases() []string {
	seen := map[string]bool{}
	var out []string
	add := func(alias string) {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if alias == "" || seen[alias] {
			return
		}
		seen[alias] = true
		out = append(out, alias)
	}
	bases := make([]string, 0, 4+len(r.ShortNames))
	bases = append(bases, r.Name, strings.ToLower(r.Kind), r.singularName(), r.pluralName())
	bases = append(bases, r.ShortNames...)
	for _, alias := range bases {
		add(alias)
		if r.Group != "" {
			add(alias + "." + r.Group)
		}
	}
	return out
}

func (r indexResource) singularName() string {
	if r.Singular != "" {
		return r.Singular
	}
	return r.Name
}

func (r indexResource) pluralName() string {
	if r.Plural != "" {
		return r.Plural
	}
	return pluralResourceName(r.Name)
}

func normalizeIndexGroup(group string) string {
	group = strings.ToLower(strings.TrimSpace(group))
	if group == "core" {
		return ""
	}
	return group
}

func normalizedNames(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func sortIndexResources(resources []indexResource) {
	sort.SliceStable(resources, func(i, j int) bool {
		return compareIndexResources(resources[i], resources[j]) < 0
	})
}

func compareIndexResources(a, b indexResource) int {
	if c := compareGroupPriority(a.Group, b.Group); c != 0 {
		return c
	}
	if c := kubeversion.CompareKubeAwareVersionStrings(a.Version, b.Version); c != 0 {
		if c > 0 {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Kind), strings.ToLower(b.Kind)); c != 0 {
		return c
	}
	if c := strings.Compare(a.pluralName(), b.pluralName()); c != 0 {
		return c
	}
	switch {
	case a.order < b.order:
		return -1
	case a.order > b.order:
		return 1
	default:
		return 0
	}
}

func compareGroupPriority(a, b string) int {
	aRank, aGroup := groupPriority(a)
	bRank, bGroup := groupPriority(b)
	if aRank != bRank {
		return aRank - bRank
	}
	return strings.Compare(aGroup, bGroup)
}

func groupPriority(group string) (int, string) {
	group = normalizeIndexGroup(group)
	for i, known := range kubernetesGroups {
		if group == known {
			return i, ""
		}
	}
	return len(kubernetesGroups), group
}

func pluralResourceName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, "y") && len(name) > 1 && !isVowel(name[len(name)-2]) {
		return strings.TrimSuffix(name, "y") + "ies"
	}
	for _, suffix := range []string{"ch", "sh", "s", "x", "z"} {
		if strings.HasSuffix(name, suffix) {
			return name + "es"
		}
	}
	return name + "s"
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

func metadataLocation(root string, elems ...string) string {
	base, tail := splitLocationTail(root)
	return strings.TrimRight(base, `/\`) + "/" + strings.Join(elems, "/") + tail
}

func splitLocationTail(location string) (string, string) {
	if idx := strings.IndexAny(location, "?#"); idx >= 0 {
		return location[:idx], location[idx:]
	}
	return location, ""
}

var completionFirstShardKeys = []string{
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
	"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
}

func completionShardKeys(prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return completionFirstShardKeys
	}
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	if !validShardKey(prefix) {
		return nil
	}
	return []string{prefix}
}

func validShardKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (t resourceTarget) matchesGroupVersion(group string, gv schema.GroupVersion, hasGV bool) bool {
	if hasGV {
		return t.Group == gv.Group && t.Version == gv.Version
	}
	return group == "" || t.Group == group
}

func (r completionResource) matchesCompletionPrefix(prefix string) bool {
	if prefix == "" || strings.HasPrefix(r.Name, prefix) {
		return true
	}
	for _, alias := range r.Aliases {
		if strings.HasPrefix(alias, prefix) {
			return true
		}
	}
	return false
}

func (e *Explainer) loadGVK(ctx context.Context, group, version, kind string) (*resolvedSchema, bool, error) {
	return e.loadGVKDepth(ctx, group, version, kind, 0)
}

func (e *Explainer) loadGVKDepth(ctx context.Context, group, version, kind string, depth int) (*resolvedSchema, bool, error) {
	if depth > maxAliasDepth {
		return nil, false, fmt.Errorf("schema alias chain exceeded %d redirects", maxAliasDepth)
	}
	vars := tmpl.SchemaVars{Group: group, Version: version, Kind: kind}
	for _, tpl := range e.templates {
		location, err := tmpl.Execute(tpl, vars)
		if err != nil {
			return nil, false, err
		}
		body, found, err := e.loadBytes(ctx, location)
		if err != nil {
			return nil, false, err
		}
		if !found {
			continue
		}
		root, err := decodeJSONMap(body)
		if err != nil {
			return nil, false, fmt.Errorf("%s: parse schema: %w", location, err)
		}
		if alias, ok := schemaAlias(root); ok {
			if alias.Group == "" {
				alias.Group = group
			}
			if alias.Version == "" {
				alias.Version = version
			}
			return e.loadGVKDepth(ctx, alias.Group, alias.Version, alias.Kind, depth+1)
		}
		resolved := &resolvedSchema{Root: root, Location: location, Group: group, Version: version, Kind: fallbackKind(kind)}
		resolved.applySchemaMetadata()
		if meta, ok := e.loadFieldIndexMetadata(ctx, location); ok {
			if meta.Kind != "" {
				resolved.Kind = meta.Kind
			}
			if meta.APIVersion != "" {
				if parsed, err := schema.ParseGroupVersion(meta.APIVersion); err == nil {
					resolved.Group = parsed.Group
					resolved.Version = parsed.Version
				}
			}
		}
		return resolved, true, nil
	}
	return nil, false, nil
}

type schemaAliasTarget struct {
	Group   string
	Version string
	Kind    string
}

func schemaAlias(root map[string]any) (schemaAliasTarget, bool) {
	raw, ok := root[keyFluxSchemaAlias].(map[string]any)
	if !ok {
		return schemaAliasTarget{}, false
	}
	kind, _ := raw["kind"].(string)
	if kind == "" {
		return schemaAliasTarget{}, false
	}
	group, _ := raw["group"].(string)
	version, _ := raw["version"].(string)
	return schemaAliasTarget{Group: group, Version: version, Kind: kind}, true
}

func (r *resolvedSchema) applySchemaMetadata() {
	if m, ok := r.Root[keyFluxSchemaGVK].(map[string]any); ok {
		if group, ok := m["group"].(string); ok {
			r.Group = group
		}
		if version, ok := m["version"].(string); ok && version != "" {
			r.Version = version
		}
		if kind, ok := m["kind"].(string); ok && kind != "" {
			r.Kind = kind
		}
	}
}

func (e *Explainer) loadFieldIndexMetadata(ctx context.Context, schemaLocation string) (fieldIndexMetadata, bool) {
	if !strings.HasSuffix(schemaLocation, ".json") {
		return fieldIndexMetadata{}, false
	}
	location := strings.TrimSuffix(schemaLocation, ".json") + ".fields.txt"
	body, found, err := e.loadBytes(ctx, location)
	if err != nil || !found {
		return fieldIndexMetadata{}, false
	}
	return parseFieldIndexMetadata(string(body)), true
}

func parseFieldIndexMetadata(data string) fieldIndexMetadata {
	var out fieldIndexMetadata
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "apiVersion <string> enum=") {
			out.APIVersion = strings.TrimSpace(strings.TrimPrefix(line, "apiVersion <string> enum="))
			if i := strings.IndexAny(out.APIVersion, " \t"); i >= 0 {
				out.APIVersion = out.APIVersion[:i]
			}
		}
		if strings.HasPrefix(line, "kind <string> enum=") {
			out.Kind = strings.TrimSpace(strings.TrimPrefix(line, "kind <string> enum="))
			if i := strings.IndexAny(out.Kind, " \t"); i >= 0 {
				out.Kind = out.Kind[:i]
			}
		}
		if out.APIVersion != "" && out.Kind != "" {
			return out
		}
	}
	return out
}

func (e *Explainer) loadBytes(ctx context.Context, location string) ([]byte, bool, error) {
	if isHTTPURL(location) {
		return e.loadHTTP(ctx, location)
	}
	b, err := os.ReadFile(location)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (e *Explainer) loadHTTP(ctx context.Context, location string) ([]byte, bool, error) {
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, false, err
	}
	if e.opts.HTTPTimeout > 0 {
		reqCtx, cancel := context.WithTimeout(ctx, e.opts.HTTPTimeout)
		defer cancel()
		req = req.WithContext(reqCtx)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("GET %s: %s", location, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

func decodeJSONMap(body []byte) (map[string]any, error) {
	var doc any
	if err := decodeJSON(body, &doc); err != nil {
		return nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("schema root must be an object")
	}
	return m, nil
}

func decodeJSON(body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(out)
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

var builtinResourceAliases = map[string][]string{
	"cm":     {"configmap"},
	"cs":     {"componentstatus"},
	"ep":     {"endpoints"},
	"ev":     {"event"},
	"limits": {"limitrange"},
	"ns":     {"namespace"},
	"no":     {"node"},
	"po":     {"pod"},
	"pvc":    {"persistentvolumeclaim"},
	"pv":     {"persistentvolume"},
	"quota":  {"resourcequota"},
	"rc":     {"replicationcontroller"},
	"sa":     {"serviceaccount"},
	"svc":    {"service"},
	"crd":    {"customresourcedefinition"},
	"crds":   {"customresourcedefinition"},
	"deploy": {"deployment"},
	"ds":     {"daemonset"},
	"rs":     {"replicaset"},
	"sts":    {"statefulset"},
	"hpa":    {"horizontalpodautoscaler"},
	"cj":     {"cronjob"},
	"ing":    {"ingress"},
	"netpol": {"networkpolicy"},
	"pdb":    {"poddisruptionbudget"},
	"pc":     {"priorityclass"},
	"sc":     {"storageclass"},
	"csr":    {"certificatesigningrequest"},
	"ip":     {"ipaddress"},
	"vac":    {"volumeattributesclass"},
}

func kindNameCandidates(resource string) []string {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return nil
	}
	lower := strings.ToLower(resource)
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	add(lower)
	for _, alias := range builtinResourceAliases[lower] {
		add(alias)
	}
	if strings.HasSuffix(lower, "ies") && len(lower) > 3 {
		add(strings.TrimSuffix(lower, "ies") + "y")
	}
	if strings.HasSuffix(lower, "es") && len(lower) > 2 {
		add(strings.TrimSuffix(lower, "es"))
	}
	if strings.HasSuffix(lower, "s") && len(lower) > 1 {
		add(strings.TrimSuffix(lower, "s"))
	}
	return out
}

func fallbackKind(kind string) string {
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

type renderer struct {
	w         io.Writer
	recursive bool
	format    string
}

func (r renderer) openAPIV2() bool {
	return r.format == OutputPlaintextOpenAPIV2
}

func (r renderer) render(resolved *resolvedSchema, fields []string) error {
	if r.openAPIV2() {
		version := resolved.Version
		if resolved.Group != "" {
			version = resolved.Group + "/" + resolved.Version
		}
		if _, err := fmt.Fprintf(r.w, "KIND:     %s\nVERSION:  %s\n\n", resolved.Kind, version); err != nil {
			return err
		}
	} else {
		if resolved.Group != "" {
			if _, err := fmt.Fprintf(r.w, "GROUP:      %s\n", resolved.Group); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(r.w, "KIND:       %s\nVERSION:    %s\n\n", resolved.Kind, resolved.Version); err != nil {
			return err
		}
	}
	if err := r.renderOutput(resolved.Root, fields); err != nil {
		return err
	}
	if r.openAPIV2() {
		return nil
	}
	_, err := fmt.Fprintln(r.w)
	return err
}

func (r renderer) renderOutput(node map[string]any, fields []string) error {
	if r.openAPIV2() {
		return r.renderOutputV2(node, fields)
	}
	if len(fields) == 0 {
		if err := r.writeDescription(node); err != nil {
			return err
		}
		fieldList := buildFieldList(node, 1, r.recursive)
		if len(fieldList) == 0 {
			return nil
		}
		if _, err := fmt.Fprintln(r.w, "FIELDS:"); err != nil {
			return err
		}
		_, err := io.WriteString(r.w, fieldList)
		return err
	}
	resolved := node
	if props := schemaMap(resolved[keyProperties]); len(props) > 0 {
		name := fields[0]
		if child := schemaMap(props[name]); child != nil {
			if len(fields) == 1 {
				if err := r.writeFieldHeader(name, child); err != nil {
					return err
				}
			}
			return r.renderOutput(child, fields[1:])
		}
	}
	if items := schemaMap(resolved[keyItems]); items != nil {
		return r.renderOutput(items, fields)
	}
	if addl := schemaMap(resolved[keyAddlProps]); addl != nil {
		return r.renderOutput(addl, fields)
	}
	for _, branch := range schemaList(resolved[keyAllOf]) {
		var buf bytes.Buffer
		branchRenderer := renderer{w: &buf, recursive: r.recursive, format: r.format}
		if err := branchRenderer.renderOutput(branch, fields); err == nil && buf.Len() > 0 {
			_, err := io.Copy(r.w, &buf)
			return err
		}
	}
	return fmt.Errorf("field %q does not exist", fields[0])
}

func (r renderer) renderOutputV2(node map[string]any, fields []string) error {
	if len(fields) == 0 {
		if err := r.writeDescriptionV2(node); err != nil {
			return err
		}
		fieldList := buildFieldListV2(node, 1, r.recursive)
		if len(fieldList) == 0 {
			return nil
		}
		if _, err := fmt.Fprintln(r.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w, "FIELDS:"); err != nil {
			return err
		}
		_, err := io.WriteString(r.w, fieldList)
		return err
	}
	if props := schemaMap(node[keyProperties]); len(props) > 0 {
		name := fields[0]
		if child := schemaMap(props[name]); child != nil {
			if len(fields) == 1 {
				return r.renderFieldV2(name, child)
			}
			return r.renderOutputV2(child, fields[1:])
		}
	}
	if items := schemaMap(node[keyItems]); items != nil {
		return r.renderOutputV2(items, fields)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		return r.renderOutputV2(addl, fields)
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		var buf bytes.Buffer
		branchRenderer := renderer{w: &buf, recursive: r.recursive, format: r.format}
		if err := branchRenderer.renderOutputV2(branch, fields); err == nil && buf.Len() > 0 {
			_, err := io.Copy(r.w, &buf)
			return err
		}
	}
	return fmt.Errorf("field %q does not exist", fields[0])
}

func (r renderer) renderFieldV2(name string, node map[string]any) error {
	fieldsNode := fieldListNode(node)
	if fieldsNode != nil {
		if _, err := fmt.Fprintf(r.w, "RESOURCE: %s <%s>\n\n", name, typeNameV2(node)); err != nil {
			return err
		}
		if err := r.writeDescriptionV2(node); err != nil {
			return err
		}
		fieldList := buildFieldListV2(fieldsNode, 1, r.recursive)
		if len(fieldList) == 0 {
			return nil
		}
		if _, err := fmt.Fprintln(r.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w, "FIELDS:"); err != nil {
			return err
		}
		_, err := io.WriteString(r.w, fieldList)
		return err
	}
	if _, err := fmt.Fprintf(r.w, "FIELD:    %s <%s>\n\n", name, typeNameV2(node)); err != nil {
		return err
	}
	return r.writeDescriptionV2(node)
}

func (r renderer) writeDescriptionV2(node map[string]any) error {
	if _, err := fmt.Fprintln(r.w, "DESCRIPTION:"); err != nil {
		return err
	}
	sections := descriptionSectionsV2(node)
	if len(sections) == 0 {
		sections = []string{"<empty>"}
	}
	for i, desc := range sections {
		if i > 0 {
			if _, err := fmt.Fprintln(r.w); err != nil {
				return err
			}
		}
		for _, line := range wrapString(desc, 75) {
			if _, err := fmt.Fprintf(r.w, "     %s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r renderer) writeFieldHeader(name string, node map[string]any) error {
	if _, err := fmt.Fprintf(r.w, "FIELD: %s <%s>\n", name, typeName(node)); err != nil {
		return err
	}
	if values := enumValues(node); len(values) > 0 {
		if _, err := fmt.Fprintln(r.w, "ENUM:"); err != nil {
			return err
		}
		for _, value := range values {
			if _, err := fmt.Fprintf(r.w, "    %s\n", enumString(value)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(r.w)
	return err
}

func (r renderer) writeDescription(node map[string]any) error {
	if _, err := fmt.Fprintln(r.w, "DESCRIPTION:"); err != nil {
		return err
	}
	desc := strings.TrimSuffix(descriptionText(node), "\n")
	if desc == "" {
		desc = "<empty>"
	}
	for _, line := range wrapString(desc, 76) {
		if _, err := fmt.Fprintf(r.w, "    %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(r.w, "    ")
	return err
}

func buildFieldListV2(node map[string]any, level int, recursive bool) string {
	var out strings.Builder
	appendFieldListV2(&out, node, level, recursive)
	return out.String()
}

func appendFieldListV2(out *strings.Builder, node map[string]any, level int, recursive bool) {
	node = fieldListNode(node)
	if node == nil {
		return
	}
	props := schemaMap(node[keyProperties])
	required := requiredSet(node[keyRequired])
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := schemaMap(props[name])
		isRequired := !recursive && required[name] && !isTopLevelIdentityField(name, level)
		writeFieldDetailV2(out, name, child, isRequired, level, recursive)
		if recursive {
			appendFieldListV2(out, child, level+1, recursive)
		}
	}
}

func fieldListNode(node map[string]any) map[string]any {
	if node == nil {
		return nil
	}
	if len(schemaMap(node[keyProperties])) > 0 {
		return node
	}
	if items := schemaMap(node[keyItems]); items != nil {
		return fieldListNode(items)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		return fieldListNode(addl)
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		if found := fieldListNode(branch); found != nil {
			return found
		}
	}
	return nil
}

func writeFieldDetailV2(out *strings.Builder, name string, node map[string]any, required bool, level int, short bool) {
	indentAmount := level * 3
	out.WriteString(strings.Repeat(" ", indentAmount))
	out.WriteString(name)
	out.WriteByte('\t')
	out.WriteByte('<')
	out.WriteString(typeNameV2(node))
	out.WriteByte('>')
	if required {
		out.WriteString(" -required-")
	}
	out.WriteByte('\n')
	if short {
		return
	}
	desc, _ := node[keyDesc].(string)
	if desc == "" {
		desc = "<no description>"
	}
	for _, line := range wrapString(desc, 80-(indentAmount+2)) {
		out.WriteString(strings.Repeat(" ", indentAmount+2))
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
}

func buildFieldList(node map[string]any, level int, recursive bool) string {
	var out strings.Builder
	appendFieldList(&out, node, level, recursive)
	return out.String()
}

func appendFieldList(out *strings.Builder, node map[string]any, level int, recursive bool) {
	if node == nil {
		return
	}
	props := schemaMap(node[keyProperties])
	if len(props) > 0 {
		required := requiredSet(node[keyRequired])
		names := make([]string, 0, len(props))
		for name := range props {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := schemaMap(props[name])
			isRequired := required[name] && !isTopLevelIdentityField(name, level)
			writeFieldDetail(out, name, child, isRequired, level, recursive)
			if recursive {
				appendFieldList(out, child, level+1, recursive)
			}
		}
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		appendFieldList(out, branch, level, recursive)
	}
	if items := schemaMap(node[keyItems]); items != nil {
		appendFieldList(out, items, level, recursive)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		appendFieldList(out, addl, level, recursive)
	}
}

func writeFieldDetail(out *strings.Builder, name string, node map[string]any, required bool, level int, short bool) {
	indentAmount := level * 2
	out.WriteString(strings.Repeat(" ", indentAmount))
	out.WriteString(name)
	out.WriteByte('\t')
	out.WriteByte('<')
	out.WriteString(typeName(node))
	out.WriteByte('>')
	if required {
		out.WriteString(" -required-")
	}
	if values := enumValues(node); len(values) > 0 {
		limit := len(values)
		truncated := false
		if short && limit > 4 {
			limit = 4
			truncated = true
		}
		out.WriteByte('\n')
		out.WriteString(strings.Repeat(" ", indentAmount))
		out.WriteString("enum: ")
		for i := 0; i < limit; i++ {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(enumString(values[i]))
		}
		if truncated {
			out.WriteString(", ....")
		}
	}
	out.WriteByte('\n')
	if short {
		return
	}
	desc, _ := node[keyDesc].(string)
	if desc == "" {
		desc = "<no description>"
	}
	for _, line := range wrapString(desc, 78-indentAmount) {
		out.WriteString(strings.Repeat(" ", indentAmount+2))
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
}

func isTopLevelIdentityField(name string, level int) bool {
	return level == 1 && (name == "apiVersion" || name == "kind")
}

func typeName(node map[string]any) string {
	if node == nil {
		return typeObject
	}
	if items := schemaMap(node[keyItems]); items != nil {
		return "[]" + typeName(items)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		return "map[string]" + typeName(addl)
	}
	if branches := schemaList(node[keyAllOf]); len(branches) == 1 && len(schemaMap(node[keyProperties])) == 0 {
		return typeName(branches[0])
	}
	if name, ok := node[keyFluxSchemaType].(string); ok && name != "" {
		return name
	}
	if t, ok := singleType(node); ok {
		if t == "object" {
			return typeObject
		}
		return t
	}
	return typeObject
}

func typeNameV2(node map[string]any) string {
	if node == nil {
		return typeObject
	}
	if items := schemaMap(node[keyItems]); items != nil {
		return "[]" + typeNameV2(items)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		return "map[string]" + typeNameV2(addl)
	}
	if branches := schemaList(node[keyAllOf]); len(branches) == 1 && len(schemaMap(node[keyProperties])) == 0 {
		return typeNameV2(branches[0])
	}
	if name, ok := node[keyFluxSchemaType].(string); ok && name != "" {
		return name
	}
	if t, ok := singleType(node); ok {
		if t == "object" {
			return typeObject
		}
		return t
	}
	return typeObject
}

func descriptionSectionsV2(node map[string]any) []string {
	var sections []string
	appendDescriptionSectionsV2(&sections, node)
	return sections
}

func appendDescriptionSectionsV2(sections *[]string, node map[string]any) {
	if node == nil {
		return
	}
	desc, _ := node[keyDesc].(string)
	appendDescriptionSection(sections, desc)
	if typeDesc, ok := node[keyFluxSchemaTypeDescription].(string); ok && !sameDescription(desc, typeDesc) {
		appendDescriptionSection(sections, typeDesc)
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		appendDescriptionSectionsV2(sections, branch)
	}
	if items := schemaMap(node[keyItems]); items != nil {
		appendDescriptionSectionsV2(sections, items)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		appendDescriptionSectionsV2(sections, addl)
	}
}

func appendDescriptionSection(sections *[]string, desc string) {
	desc = strings.TrimSuffix(desc, "\n")
	if desc == "" {
		return
	}
	*sections = append(*sections, desc)
}

func descriptionText(node map[string]any) string {
	var b strings.Builder
	appendDescription(&b, node)
	return b.String()
}

func appendDescription(out *strings.Builder, node map[string]any) {
	if node == nil {
		return
	}
	desc, _ := node[keyDesc].(string)
	appendDescriptionString(out, desc)
	if typeDesc, ok := node[keyFluxSchemaTypeDescription].(string); ok && !sameDescription(desc, typeDesc) {
		appendDescriptionString(out, typeDesc)
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		appendDescription(out, branch)
	}
	if items := schemaMap(node[keyItems]); items != nil {
		appendDescription(out, items)
	}
	if addl := schemaMap(node[keyAddlProps]); addl != nil {
		appendDescription(out, addl)
	}
}

func appendDescriptionString(out *strings.Builder, desc string) {
	if desc == "" {
		return
	}
	out.WriteString(desc)
	out.WriteByte('\n')
}

func sameDescription(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func schemaMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func schemaList(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func requiredSet(v any) map[string]bool {
	out := map[string]bool{}
	arr, _ := v.([]any)
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out[s] = true
		}
	}
	return out
}

func singleType(node map[string]any) (string, bool) {
	switch t := node[keyType].(type) {
	case string:
		return t, true
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "null" {
				return s, true
			}
		}
	}
	return "", false
}

func enumValues(node map[string]any) []any {
	if node == nil {
		return nil
	}
	if arr, ok := node[keyEnum].([]any); ok {
		return arr
	}
	for _, branch := range schemaList(node[keyAllOf]) {
		if arr := enumValues(branch); len(arr) > 0 {
			return arr
		}
	}
	return nil
}

func enumString(v any) string {
	if s, ok := v.(string); ok && s == "" {
		return `""`
	}
	return fmt.Sprint(v)
}

type line struct {
	wrap  int
	words []string
}

func (l *line) String() string { return strings.Join(l.words, " ") }
func (l *line) Empty() bool    { return len(l.words) == 0 }
func (l *line) Len() int       { return len(l.String()) }

func (l *line) Add(word string) bool {
	newLine := line{wrap: l.wrap, words: append(l.words, word)}
	if newLine.Len() <= l.wrap || len(l.words) == 0 {
		l.words = newLine.words
		return true
	}
	return false
}

var bullet = regexp.MustCompile(`^(\d+\.?|-|\*)\s`)

func shouldStartNewLine(lastWord, str string) bool {
	if strings.HasSuffix(lastWord, ":") {
		return true
	}
	if strings.HasPrefix(str, "    ") {
		return true
	}
	str = strings.TrimSpace(str)
	if len(str) == 0 {
		return true
	}
	return bullet.MatchString(str)
}

func wrapString(str string, wrap int) []string {
	var wrapped []string
	l := line{wrap: wrap}
	lastWord := ""
	flush := func() {
		if !l.Empty() {
			lastWord = ""
			wrapped = append(wrapped, l.String())
			l = line{wrap: wrap}
		}
	}
	for _, s := range strings.Split(str, "\n") {
		if strings.HasPrefix(s, "    ") {
			flush()
			wrapped = append(wrapped, s)
			continue
		}
		if len(wrapped) > 0 && len(strings.TrimSpace(s)) == 0 {
			flush()
			wrapped = append(wrapped, "")
			continue
		}
		if shouldStartNewLine(lastWord, s) {
			flush()
		}
		for _, word := range strings.Fields(s) {
			lastWord = word
			if !l.Add(word) {
				flush()
				if !l.Add(word) {
					panic("could not add word to empty line")
				}
			}
		}
	}
	flush()
	return wrapped
}
