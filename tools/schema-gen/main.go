// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"
)

func main() {
	inputPath := flag.String("in", "", "controller-gen CRD YAML input path, or '-' for stdin")
	outputPath := flag.String("out", "", "standalone JSON Schema output path")
	controllerGen := flag.String("controller-gen", "controller-gen", "controller-gen binary path")
	group := flag.String("group", "", "Kubernetes API group")
	version := flag.String("version", "", "Kubernetes API version")
	kind := flag.String("kind", "", "Kubernetes kind")
	apiType := flag.String("type", "", "Go API type to wrap, in import/path.Type form")
	field := flag.String("field", "", "JSON field name for the wrapped API type")
	scope := flag.String("scope", "Cluster", "CRD resource scope")
	schemaField := flag.Bool("schema-field", false, "include optional root $schema field")
	schemaID := flag.String("id", "", "JSON Schema $id")
	flag.Parse()

	if *outputPath == "" {
		exit(errors.New("-out is required"))
	}
	if *schemaID == "" {
		exit(errors.New("-id is required"))
	}
	opts := schemaOptions{
		controllerGen:    *controllerGen,
		group:            *group,
		version:          *version,
		kind:             *kind,
		apiType:          *apiType,
		field:            *field,
		scope:            *scope,
		schemaField:      *schemaField,
		id:               *schemaID,
		controllerGenYML: *inputPath,
	}
	if err := opts.validate(); err != nil {
		exit(err)
	}

	input, err := opts.controllerGenOutput()
	if err != nil {
		exit(err)
	}

	schema, err := buildSchema(input, opts)
	if err != nil {
		exit(err)
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		exit(fmt.Errorf("marshal report schema: %w", err))
	}
	data = append(data, '\n')

	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		exit(fmt.Errorf("write %s: %w", *outputPath, err))
	}
}

func exit(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "schema-gen: %v\n", err)
	os.Exit(1)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

type schemaOptions struct {
	controllerGen    string
	group            string
	version          string
	kind             string
	apiType          string
	field            string
	scope            string
	schemaField      bool
	id               string
	controllerGenYML string
}

type crdIdentity struct {
	apiVersion string
	kind       string
}

func (o schemaOptions) validate() error {
	if o.field == "" {
		return errors.New("-field is required")
	}
	if o.controllerGenYML != "" {
		return nil
	}
	switch {
	case o.controllerGen == "":
		return errors.New("-controller-gen is required")
	case o.group == "":
		return errors.New("-group is required")
	case o.version == "":
		return errors.New("-version is required")
	case o.kind == "":
		return errors.New("-kind is required")
	case o.apiType == "":
		return errors.New("-type is required")
	case !strings.Contains(o.apiType, "."):
		return fmt.Errorf("-type must be in import/path.Type form, got %q", o.apiType)
	}
	return nil
}

func (o schemaOptions) controllerGenOutput() ([]byte, error) {
	if o.controllerGenYML != "" {
		return readInput(o.controllerGenYML)
	}
	dir, err := os.MkdirTemp(".", ".schema-gen-")
	if err != nil {
		return nil, fmt.Errorf("create temp package: %w", err)
	}
	defer os.RemoveAll(dir)

	if err := o.writeWrapperPackage(dir); err != nil {
		return nil, err
	}

	cmd := exec.Command(o.controllerGen, "crd", "paths="+dir, "output:stdout")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("run controller-gen: %s", msg)
	}
	return out, nil
}

func (o schemaOptions) writeWrapperPackage(dir string) error {
	importPath, typeName, ok := splitGoType(o.apiType)
	if !ok {
		return fmt.Errorf("-type must be in import/path.Type form, got %q", o.apiType)
	}
	fieldName := exportName(o.field)
	plural := strings.ToLower(o.kind) + "s"
	schemaField := ""
	if o.schemaField {
		schemaField = `
	// +optional
	Schema string ` + "`json:\"$schema,omitempty\"`" + `
`
	}
	doc := fmt.Sprintf(`// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// +groupName=%s
// +versionName=%s
// +kubebuilder:object:generate=false
package schemagen
`, o.group, o.version)
	types := fmt.Sprintf(`// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package schemagen

import (
	api "%s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=%s,scope=%s
type %s struct {
	metav1.TypeMeta   `+"`json:\",inline\"`"+`
	metav1.ObjectMeta `+"`json:\"metadata,omitempty\"`"+`
%s
	%s api.%s `+"`json:\"%s\"`"+`
}
`, importPath, plural, o.scope, o.kind, schemaField, fieldName, typeName, o.field)
	if err := os.WriteFile(filepath.Join(dir, "doc.go"), []byte(doc), 0o644); err != nil {
		return fmt.Errorf("write wrapper doc.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(types), 0o644); err != nil {
		return fmt.Errorf("write wrapper types.go: %w", err)
	}
	return nil
}

func splitGoType(s string) (importPath, typeName string, ok bool) {
	slash := strings.LastIndex(s, "/")
	dot := strings.LastIndex(s, ".")
	if dot <= slash || dot == len(s)-1 {
		return "", "", false
	}
	return s[:dot], s[dot+1:], true
}

func exportName(s string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range s {
		if r == '-' || r == '_' || r == '.' {
			upperNext = true
			continue
		}
		if upperNext {
			r = unicode.ToUpper(r)
			upperNext = false
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "Value"
	}
	return b.String()
}

func buildSchema(input []byte, opts schemaOptions) (map[string]any, error) {
	var crd map[string]any
	if err := yaml.Unmarshal(input, &crd); err != nil {
		return nil, fmt.Errorf("parse controller-gen output: %w", err)
	}

	identity, err := extractCRDIdentity(crd)
	if err != nil {
		return nil, err
	}

	openAPI, err := extractOpenAPISchema(crd)
	if err != nil {
		return nil, err
	}

	schema, ok := transformNode(cloneMap(openAPI)).(map[string]any)
	if !ok {
		return nil, errors.New("openAPIV3Schema is not an object")
	}

	if err := rewriteRootSchema(schema, identity, opts); err != nil {
		return nil, err
	}

	return schema, nil
}

func extractCRDIdentity(crd map[string]any) (crdIdentity, error) {
	spec, err := requiredMap(crd, "spec")
	if err != nil {
		return crdIdentity{}, err
	}
	group, err := requiredString(spec, "group")
	if err != nil {
		return crdIdentity{}, fmt.Errorf("spec: %w", err)
	}
	names, err := requiredMap(spec, "names")
	if err != nil {
		return crdIdentity{}, fmt.Errorf("spec: %w", err)
	}
	kind, err := requiredString(names, "kind")
	if err != nil {
		return crdIdentity{}, fmt.Errorf("spec.names: %w", err)
	}
	versions, err := requiredSlice(spec, "versions")
	if err != nil {
		return crdIdentity{}, err
	}
	if len(versions) == 0 {
		return crdIdentity{}, errors.New("spec.versions is empty")
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		return crdIdentity{}, errors.New("spec.versions[0] is not an object")
	}
	versionName, err := requiredString(version, "name")
	if err != nil {
		return crdIdentity{}, fmt.Errorf("spec.versions[0]: %w", err)
	}
	return crdIdentity{
		apiVersion: group + "/" + versionName,
		kind:       kind,
	}, nil
}

func extractOpenAPISchema(crd map[string]any) (map[string]any, error) {
	spec, err := requiredMap(crd, "spec")
	if err != nil {
		return nil, err
	}
	versions, err := requiredSlice(spec, "versions")
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, errors.New("spec.versions is empty")
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		return nil, errors.New("spec.versions[0] is not an object")
	}
	schema, err := requiredMap(version, "schema")
	if err != nil {
		return nil, fmt.Errorf("spec.versions[0]: %w", err)
	}
	openAPI, err := requiredMap(schema, "openAPIV3Schema")
	if err != nil {
		return nil, fmt.Errorf("spec.versions[0].schema: %w", err)
	}
	return openAPI, nil
}

func rewriteRootSchema(schema map[string]any, identity crdIdentity, opts schemaOptions) error {
	props, err := requiredMap(schema, "properties")
	if err != nil {
		return err
	}
	delete(props, "metadata")

	apiVersion, err := requiredMap(props, "apiVersion")
	if err != nil {
		return fmt.Errorf("properties: %w", err)
	}
	apiVersion["const"] = identity.apiVersion
	delete(apiVersion, "description")

	kind, err := requiredMap(props, "kind")
	if err != nil {
		return fmt.Errorf("properties: %w", err)
	}
	kind["const"] = identity.kind
	delete(kind, "description")

	if opts.schemaField {
		schemaProp, err := requiredMap(props, "$schema")
		if err != nil {
			return fmt.Errorf("properties: %w", err)
		}
		schemaProp["format"] = "uri"
	}

	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = opts.id
	wrappedField, err := requiredMap(props, opts.field)
	if err != nil {
		return fmt.Errorf("properties: %w", err)
	}
	if title, ok := wrappedField["description"].(string); ok && title != "" {
		schema["title"] = title
	}
	schema["additionalProperties"] = false
	schema["required"] = []any{"apiVersion", "kind", opts.field}
	return nil
}

func transformNode(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			x[k] = transformNode(child)
		}
		if x["type"] == "object" {
			if _, ok := x["properties"]; ok {
				x["additionalProperties"] = false
			}
		}
		if nullable, _ := x["nullable"].(bool); nullable {
			delete(x, "nullable")
			inner := cloneMap(x)
			out := map[string]any{
				"oneOf": []any{
					map[string]any{"type": "null"},
					inner,
				},
			}
			if desc, ok := x["description"]; ok {
				out["description"] = desc
				delete(inner, "description")
			}
			return out
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = transformNode(child)
		}
		return x
	default:
		return v
	}
}

func requiredMap(parent map[string]any, key string) (map[string]any, error) {
	v, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("missing %q", key)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%q is not an object", key)
	}
	return m, nil
}

func requiredSlice(parent map[string]any, key string) ([]any, error) {
	v, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("missing %q", key)
	}
	s, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%q is not an array", key)
	}
	return s, nil
}

func requiredString(parent map[string]any, key string) (string, error) {
	v, ok := parent[key]
	if !ok {
		return "", fmt.Errorf("missing %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q is not a string", key)
	}
	return s, nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return x
	}
}
