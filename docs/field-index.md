---
weight: 45
---

# Field Indexes for AI Agents

Pass `--with-field-index` to `extract crd`, `extract k8s`, or
`extract openshift` to write greppable, LLM-friendly field indexes alongside
the extracted schemas. Each index is a plain-text UTF-8 file with one
self-contained line per field: full dotted path, type, constraints, and
description when available. An agent greps a dotted path or keyword instead
of reading the full JSON Schema.

The index serves agents in three roles:

- **Authoring manifests** — the `spec` subtree tells the agent which fields
  exist, which are `(required)`, and which values are legal (`enum=`,
  `pattern=`, `min=`/`max=`, `default=`), so generated YAML passes
  `flux schema validate` on the first attempt.
- **Assessing resources** — the `status` subtree tells the agent what the
  controller reports, for interpreting the state of a resource and for
  authoring expressions against it: health check CEL (`healthCheckExprs`,
  `readyExpr`), monitoring queries, and troubleshooting. `(immutable)`
  markers tell the agent when a change requires recreating the resource
  instead of patching it.
- **Understanding semantics** — the descriptions carry what the schema
  cannot express: field precedence, mutual exclusions, units, and expected
  values documented only in prose. For fields outside the agent's training
  data — new API versions or private CRDs — the description line is its only
  source of meaning, grounding answers in the upstream documentation instead
  of guesswork.

Compared to `kubectl explain`, the index answers offline without a cluster or
installed CRD, in one self-contained line instead of a per-path query, keeps
descriptions in full-tree listings (where `explain --recursive` drops them),
surfaces constraints systematically, and supports keyword search across all
fields and kinds — the agent does not need to know a field's path to find it.

## File naming

The index path is derived from the rendered schema path. If `--output-format`
renders a path ending in `.json`, that suffix is replaced with `.fields.txt`;
otherwise `.fields.txt` is appended. For example,
`helm.toolkit.fluxcd.io/helmrelease_v2.json` gets
`helm.toolkit.fluxcd.io/helmrelease_v2.fields.txt`.

## Comments

Lines beginning with `# ` are comments: they carry metadata about the index
rather than field definitions, so tools that parse the line grammar must skip
them (and tolerate comment keys added in the future). When known, an index
begins with `# schema source: <value>`; when unknown, the comment is omitted
and the index starts directly with the `apiVersion` line.

The schema source value is composed by command: `extract crd` joins the CRD
labels `app.kubernetes.io/part-of` and `app.kubernetes.io/version` when
present, `extract k8s` records `Kubernetes <info.version>` (or `Kubernetes`,
with `--version` appended when the swagger has no real version), and
`extract openshift` records `OpenShift <info.version>` (or `OpenShift`, with
`--ref` appended when the swagger has no version). Pass `--index-source` to
override auto-detection for every index in an invocation.

## Line grammar

Each field line has `path <type>`, optional annotations, then an optional
description separated by one TAB and written as `# description`. Tokens before
the description are separated by one space:

```text
line         = comment-line | field-line
comment-line = "# " text
field-line   = path SPACE type [SPACE "(required)"] [SPACE enum]
               [SPACE default] [SPACE format] [SPACE pattern]
               [SPACE min] [SPACE max] [SPACE "(immutable)"]
               [SPACE "(deprecated)"] [SPACE "(cluster-scoped)"]
               [TAB "# " description]

path         = segment *( "." segment )
segment      = name | name "[]" | "<key>"
type         = "<" typename ">"
enum         = "enum=" value *( "|" value )
default      = "default=" json-value
format       = "format=" value
pattern      = "pattern=" json-string
min          = "min=" json-number
max          = "max=" json-number
```

`# schema source:` is the only comment key currently emitted; parsers must
tolerate other keys. `(deprecated)` appears only on the `apiVersion` header
line and `(cluster-scoped)` only on the `kind` header line.

Two guarantees matter for parsing. Descriptions (including deprecation
warnings) are whitespace-normalized — newlines and tabs collapse to single
spaces — so a line contains at most one TAB, and splitting on the first TAB
always separates the tokens from the description. Naive space-splitting of
the tokens is unsafe: the type token may contain a space
(`<object (free-form)>`), and JSON-encoded annotation values may too
(`pattern="^foo bar$"`); parse the type as a `<...>`-delimited token and
treat quoted annotation values as JSON strings.

Example lines from a HelmRelease index:

```text
# schema source: flux v2.8.5
apiVersion <string> enum=helm.toolkit.fluxcd.io/v2
kind <string> enum=HelmRelease
metadata.name <string> (required)
metadata.namespace <string> (required)
spec <object>	# HelmReleaseSpec defines the desired state of a Helm release.
spec.valuesFrom <[]object>	# ValuesFrom holds references to resources containing Helm values for this HelmRelease, and information about how they should be merged.
spec.valuesFrom[].kind <string> (required) enum=Secret|ConfigMap	# Kind of the values referent, valid values are ('Secret', 'ConfigMap').
spec.valuesFrom[].name <string> (required) min=1 max=253	# Name of the values referent. Should reside in the same namespace as the referring resource.
```

## Path notation

| Notation   | Meaning                                                   | Example                                            |
|------------|-----------------------------------------------------------|----------------------------------------------------|
| `a.b.c`    | Nested object field.                                      | `spec.chart.spec`                                  |
| `a[]`      | Array of objects; child fields continue below the array.  | `spec.dependsOn[]` -> `spec.dependsOn[].name`      |
| `a.<key>.` | Object map value fields.                                  | `spec.devices[].attributes.<key>.version <string>` |

The `.<key>.` segment means "for each key in this map"; it is used when a map
has object values with known fields.

## Type notation

Type notation uses Kubernetes schema types in angle brackets:

- `<string>`, `<integer>`, `<number>`, `<boolean>` — scalar fields.
- `<object>` — object with known fields, listed below it.
- `<object (free-form)>` — object without a defined structure.
- `<[]string>` (or the matching scalar type) — array of scalars.
- `<[]object>` — array of objects; child fields continue under `a[].`.
- `<map[string]string>` (or the matching value type) — map with scalar values.
- `<map[string]object>` — map with object values; child fields, when known,
  continue under `.<key>.`.
- `<int-or-string>` — accepts an integer or a string.
- `<any>` — untyped (e.g. preserves unknown fields).

## Annotations

Annotations are suffix tokens after the type. Facts that carry a value use the
`key=value` form; boolean facts are parenthesized markers whose presence means
true and whose absence means false.

Key-value annotations:

- `enum=a|b|c` — allowed values; values containing whitespace or `|` are
  JSON-encoded, all other values stay verbatim.
- `default=<json>` — the schema default, JSON-encoded.
- `format=<value>` — the schema format (e.g. `date-time`).
- `pattern=<json-string>` — the regex pattern, always JSON-encoded.
- `min=<n>` / `max=<n>` — minimum/maximum for number and integer fields,
  minLength/maxLength for strings, minItems/maxItems for arrays.

Boolean markers:

- `(required)` — the field appears in the parent schema's required list.
- `(immutable)` — the field carries a CEL validation rule equal to
  `self == oldSelf` (either operand order), or its description contains
  `Cannot be updated.`.
- `(deprecated)` — on the apiVersion header line only, when the CRD version is
  deprecated; the deprecation warning is appended as its description when
  present.
- `(cluster-scoped)` — on the kind header line only, when the resource is
  cluster-scoped; such indexes have no `metadata.namespace` line.

## Header lines

After the optional schema source comment, every index starts with header lines
for `apiVersion`, `kind`, `metadata.name`, and, when applicable,
`metadata.namespace`. `apiVersion` and `kind` use `enum=` values from the
extracted GVK, and `metadata.name` is always emitted with `(required)`. The
resource scope determines the rest:

- namespaced kinds: `metadata.namespace <string> (required)` is emitted.
- cluster-scoped kinds: the `metadata.namespace` line is omitted and the kind
  line ends with `(cluster-scoped)`.
- unknown scope: `metadata.namespace <string>` is emitted without `(required)`.

`extract crd` reads exact scope from `spec.scope`; `extract k8s` and
`extract openshift` derive it from swagger paths (the openshift/api document
carries none, so its scope comes out unknown).

The header lines replace the schema's own `apiVersion`, `kind`, and
`metadata` properties, which are suppressed from the body: fields like
`metadata.labels` and `metadata.annotations` are standard across all kinds
and deliberately not listed — their absence from an index does not mean they
are unsupported.

## Generation notes

Indexes are generated before `--strip-description` is applied, so field index
descriptions are preserved even when the JSON Schemas are stripped.
For `extract k8s` and `extract openshift`, schemas whose kind ends in `List`
still write JSON Schema files but do not write `.fields.txt` indexes.

## Grep recipes

```shell
grep '^spec\.chart\.' helmrelease_v2.fields.txt # subtree listing
grep 'valuesFrom' helmrelease_v2.fields.txt     # locate a field
grep '(required)' helmrelease_v2.fields.txt     # all required fields
grep '^status\.' helmrelease_v2.fields.txt      # fields reported by the controller
grep -r 'certSecretRef' ./my-catalog            # which kinds support custom TLS certs
```
