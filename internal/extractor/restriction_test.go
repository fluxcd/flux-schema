// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/flux-schema/internal/extractor"
)

func TestExtractRestriction_Example1(t *testing.T) {
	g := NewWithT(t)

	yamlInput := []byte(`
apiVersion: my.group.io/v1alpha1
kind: MyKind
restriction: my-restriction

spec:
  foo: $string                  # required string
  baz: qux                      # const, optional
  replicas: 3                   # const, optional
  name: $string(pattern=^[a-z][a-z0-9-]*$)
  size: $enum[Small, Medium, Large]
  port: $integer(min=1, max=65535)
  description: $string?         # optional string
  legacy: $forbid               # must be absent
  $required: [baz]              # baz must be present
  config:
    $closed: true               # no other keys allowed
    enabled: $boolean
`)

	schemas, errs := extractor.ExtractRestriction(yamlInput)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))

	s := schemas[0]
	g.Expect(s.Group).To(Equal("my.group.io"))
	g.Expect(s.Version).To(Equal("v1alpha1"))
	g.Expect(s.Kind).To(Equal("MyKind"))
	g.Expect(s.Restriction).To(Equal("my-restriction"))

	expectedJSON := `{
  "type": "object",
  "properties": {
    "apiVersion": { "type": "string", "const": "my.group.io/v1alpha1" },
    "kind": { "type": "string", "const": "MyKind" },
    "spec": {
      "type": "object",
      "properties": {
        "foo": { "type": "string" },
        "baz": { "type": "string", "const": "qux" },
        "replicas": { "type": "integer", "const": 3 },
        "name": { "type": "string", "pattern": "^[a-z][a-z0-9-]*$" },
        "size": { "type": "string", "enum": ["Small", "Medium", "Large"] },
        "port": { "type": "integer", "minimum": 1, "maximum": 65535 },
        "description": { "type": "string" },
        "legacy": false,
        "config": {
          "type": "object",
          "properties": {
            "enabled": { "type": "boolean" }
          },
          "required": ["enabled"],
          "additionalProperties": false
        }
      },
      "required": ["baz", "foo", "name", "port", "size"]
    }
  },
  "required": ["apiVersion", "kind"]
}`

	var expected, actual any
	g.Expect(json.Unmarshal([]byte(expectedJSON), &expected)).To(Succeed())
	actualBytes, err := json.Marshal(s.JSON)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(json.Unmarshal(actualBytes, &actual)).To(Succeed())

	g.Expect(actual).To(Equal(expected))
}

func TestExtractRestriction_Example2(t *testing.T) {
	g := NewWithT(t)

	yamlInput := []byte(`
apiVersion: fluxcd.controlplane.io/v1
kind: ResourceSetInputProvider
restriction: app-bundle

metadata:
  name: $string
  namespace: $forbid

spec:
  type: Static
  defaultValues:
    tenant: $string
    app:
      version: $string
      replicas: 2
`)

	schemas, errs := extractor.ExtractRestriction(yamlInput)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))

	s := schemas[0]
	g.Expect(s.Group).To(Equal("fluxcd.controlplane.io"))
	g.Expect(s.Version).To(Equal("v1"))
	g.Expect(s.Kind).To(Equal("ResourceSetInputProvider"))
	g.Expect(s.Restriction).To(Equal("app-bundle"))

	expectedJSON := `{
  "type": "object",
  "properties": {
    "apiVersion": { "type": "string", "const": "fluxcd.controlplane.io/v1" },
    "kind": { "type": "string", "const": "ResourceSetInputProvider" },
    "metadata": {
      "type": "object",
      "properties": {
        "name": { "type": "string" },
        "namespace": false
      },
      "required": ["name"]
    },
    "spec": {
      "type": "object",
      "properties": {
        "type": { "type": "string", "const": "Static" },
        "defaultValues": {
          "type": "object",
          "properties": {
            "tenant": { "type": "string" },
            "app": {
              "type": "object",
              "properties": {
                "version": { "type": "string" },
                "replicas": { "type": "integer", "const": 2 }
              },
              "required": ["version"]
            }
          },
          "required": ["tenant"]
        }
      }
    }
  },
  "required": ["apiVersion", "kind"]
}`

	var expected, actual any
	g.Expect(json.Unmarshal([]byte(expectedJSON), &expected)).To(Succeed())
	actualBytes, err := json.Marshal(s.JSON)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(json.Unmarshal(actualBytes, &actual)).To(Succeed())

	g.Expect(actual).To(Equal(expected))
}

func TestExtractRestriction_Errors(t *testing.T) {
	g := NewWithT(t)

	// Missing apiVersion
	_, errs := extractor.ExtractRestriction([]byte(`
kind: MyKind
restriction: my-restr
`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("missing required 'apiVersion'"))

	// Missing kind
	_, errs = extractor.ExtractRestriction([]byte(`
apiVersion: v1
restriction: my-restr
`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("missing required 'kind'"))

	// Missing restriction
	_, errs = extractor.ExtractRestriction([]byte(`
apiVersion: v1
kind: Pod
`))
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("missing required 'restriction'"))
}

func TestExtractRestriction_MultipleDocs(t *testing.T) {
	g := NewWithT(t)

	yamlInput := []byte(`
apiVersion: v1
kind: ConfigMap
restriction: cm-restr
data:
  key: val
---
apiVersion: apps/v1
kind: Deployment
restriction: dep-restr
spec:
  replicas: 1
`)

	schemas, errs := extractor.ExtractRestriction(yamlInput)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(2))
	g.Expect(schemas[0].Kind).To(Equal("ConfigMap"))
	g.Expect(schemas[0].Restriction).To(Equal("cm-restr"))
	g.Expect(schemas[1].Kind).To(Equal("Deployment"))
	g.Expect(schemas[1].Restriction).To(Equal("dep-restr"))
}

func TestExtractRestriction_TypesAndPlaceholders(t *testing.T) {
	g := NewWithT(t)

	yamlInput := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  optStr: $string?
  reqInt: $int
  optNum: $number?
  reqBool: $bool
  escapedDollar: $$literal
  flag: true
  pi: 3.14
  tags:
    - $string
`)

	schemas, errs := extractor.ExtractRestriction(yamlInput)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))

	spec := schemas[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)
	props := spec["properties"].(map[string]any)
	required := spec["required"].([]any)

	g.Expect(props["optStr"]).To(Equal(map[string]any{"type": "string"}))
	g.Expect(props["reqInt"]).To(Equal(map[string]any{"type": "integer"}))
	g.Expect(props["optNum"]).To(Equal(map[string]any{"type": "number"}))
	g.Expect(props["reqBool"]).To(Equal(map[string]any{"type": "boolean"}))
	g.Expect(props["escapedDollar"]).To(Equal(map[string]any{"type": "string", "const": "$literal"}))
	g.Expect(props["flag"]).To(Equal(map[string]any{"type": "boolean", "const": true}))
	g.Expect(props["pi"]).To(Equal(map[string]any{"type": "number", "const": 3.14}))

	g.Expect(required).To(ContainElement("reqInt"))
	g.Expect(required).To(ContainElement("reqBool"))
	g.Expect(required).To(ContainElement("tags"))
	g.Expect(required).ToNot(ContainElement("optStr"))
	g.Expect(required).ToNot(ContainElement("optNum"))
}

func TestExtractRestriction_MultiElementArrayValidation(t *testing.T) {
	g := NewWithT(t)

	// Object array with more than one element should be rejected
	multiObjArray := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  containers:
    - name: $string
    - name: $string
`)
	_, errs := extractor.ExtractRestriction(multiObjArray)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("object arrays with more than one element are not supported"))

	// Primitive array with same types should be accepted
	multiPrimitiveArray := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  tags:
    - foo
    - bar
`)
	schemas, errs := extractor.ExtractRestriction(multiPrimitiveArray)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	tagsSchema := schemas[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["tags"].(map[string]any)
	g.Expect(tagsSchema["type"]).To(Equal("array"))
	g.Expect(tagsSchema["items"]).To(Equal(map[string]any{"type": "string"}))

	// Primitive array with mixed types should be rejected
	mixedPrimitiveArray := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  tags:
    - foo
    - 123
`)
	_, errs = extractor.ExtractRestriction(mixedPrimitiveArray)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("array elements must have the same type"))
}

func TestExtractRestriction_UnknownDirectives(t *testing.T) {
	g := NewWithT(t)

	// Typo in directive value (Finding 2 example)
	typoDirective := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  replicas: $intger
`)
	_, errs := extractor.ExtractRestriction(typoDirective)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring(`unknown directive "$intger"`))

	// Unknown directive key
	unknownKey := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  $requierd: [replicas]
  replicas: 1
`)
	_, errs = extractor.ExtractRestriction(unknownKey)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring(`unknown directive "$requierd"`))

	// Unknown argument for type
	unknownArg := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  name: $string(foo=bar)
`)
	_, errs = extractor.ExtractRestriction(unknownArg)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring(`unknown argument "foo" for type "string"`))
}

func TestExtractRestriction_RegexQuantifiersInPattern(t *testing.T) {
	g := NewWithT(t)

	// Quantifier {2,4} with comma (Finding 3 example)
	patternQuantifier := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  name: $string(pattern=^[a-z]{2,4}$)
`)
	schemas, errs := extractor.ExtractRestriction(patternQuantifier)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	nameProp := schemas[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["name"].(map[string]any)
	g.Expect(nameProp["type"]).To(Equal("string"))
	g.Expect(nameProp["pattern"]).To(Equal("^[a-z]{2,4}$"))

	// Preceding arguments before pattern
	multiArgs := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  name: $string(minlength=2, pattern=^[a-z]{2,4}$)
`)
	schemas, errs = extractor.ExtractRestriction(multiArgs)
	g.Expect(errs).To(BeEmpty())
	g.Expect(schemas).To(HaveLen(1))
	nameProp = schemas[0].JSON["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["name"].(map[string]any)
	g.Expect(nameProp["type"]).To(Equal("string"))
	g.Expect(nameProp["minLength"]).To(Equal(int64(2)))
	g.Expect(nameProp["pattern"]).To(Equal("^[a-z]{2,4}$"))

	// Pattern not as last argument should fail
	patternNotLast := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  name: $string(pattern=^[a-z]+$, minlength=2)
`)
	_, errs = extractor.ExtractRestriction(patternNotLast)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("pattern must be the last argument"))

	// Invalid regex pattern should fail
	invalidRegex := []byte(`
apiVersion: test.io/v1
kind: TestKind
restriction: test-restr

spec:
  name: $string(pattern=[a-z)
`)
	_, errs = extractor.ExtractRestriction(invalidRegex)
	g.Expect(errs).To(HaveLen(1))
	g.Expect(errs[0].Error()).To(ContainSubstring("invalid pattern regex"))
}
