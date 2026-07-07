---
weight: 40
---

# Maintaining Custom Catalogs with Flux Schema CLI

A catalog is a directory tree of JSON Schemas laid out so that
`flux schema validate` can resolve a schema from a manifest's `apiVersion`
and `kind`. The built-in [Flux Schema catalog](../catalog/README.md) ships
schemas for Kubernetes, OpenShift, Gateway API, and the Flux ecosystem;
custom catalogs let you cover internal CRDs, pin specific upstream versions,
or assemble a layout that fits your tooling.
For third-party CRDs you don't want to extract yourself, the hosted
[ecosystem catalog](https://schemas.fluxoperator.dev) is available via
`--schema-location ecosystem`.

## Layout

The default resolution template is:

```
{{ .Group }}/{{ .Kind }}_{{ .Version }}.json
```

For example, `apiVersion: source.toolkit.fluxcd.io/v1`, `kind: GitRepository`
resolves to `source.toolkit.fluxcd.io/GitRepository_v1.json`.

All template variables are lowercased at render time, so the file names on
disk are lowercase even though `apiVersion`/`kind` are mixed case.

If you need a different layout (for example the kubeconform/kubeval flat
form), pass a full template to `--schema-location`:

```shell
flux schema validate ./manifests \
  --schema-location './my-catalog/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'
```

Supported template variables:

| Variable       | Example                    |
|----------------|----------------------------|
| `.Group`       | `source.toolkit.fluxcd.io` |
| `.GroupPrefix` | `source`                   |
| `.Kind`        | `gitrepository`            |
| `.Version`     | `v1`                       |

An empty `.Group` (Kubernetes core API, e.g. `apiVersion: v1`) is normalized
to `core`, and `.GroupPrefix` is derived from `.Group` when unset. So a core
`Pod` renders as `core/pod_v1.json` with the default template and resolves to
the same path when `validate` looks up its schema.

The `extract` commands described below all share the same default template
and `-f, --output-format` flag, so a single `--schema-location` resolves
everything you generate.

## Populating a catalog

The `flux schema extract` commands write one JSON Schema file per kind, using
the layout above by default. Mix and match these commands to cover the
Kubernetes core APIs, OpenShift resources, and any CRDs your manifests rely
on.

### Kubernetes CRDs

```shell
flux schema extract crd [files...]
```

Reads bare CRDs, a `List` of CRDs, or a multi-document YAML stream, and
emits one schema file per CRD version.

| Flag                   | Description                                                                                  |
|------------------------|----------------------------------------------------------------------------------------------|
| `-d, --output-dir`     | Directory to write JSON Schema files to (mutually exclusive with `--output-archive`).        |
| `-a, --output-archive` | Path to write a gzipped tar archive of JSON Schema files to.                                 |
| `-f, --output-format`  | Go template for output file paths (default: `{{ .Group }}/{{ .Kind }}_{{ .Version }}.json`). |
| `--index-source`          | Source name and version recorded in field index headers, overriding auto-detection.      |
| `--strip-description`     | Drop `description` fields from the generated schemas to reduce their size.               |
| `--with-field-index`      | Also write a `.fields.txt` field index next to each schema.                              |
| `--with-explain-metadata` | Include `x-flux-schema-*` annotations, alias redirects, and `.explain/` lookup shards used by `flux schema explain`. |

Generate schemas for every CRD installed in a cluster:

```shell
kubectl get crds -o yaml | flux schema extract crd -d ./my-catalog
```

Or from a kustomize build of an operator's CRD bundle:

```shell
kustomize build ./config/crd | flux schema extract crd -d ./my-catalog
```

> The output is also compatible with `kubeconform` and `kubeval`, making
> `extract crd` a drop-in replacement for kubeconform's
> [openapi2jsonschema.py](https://github.com/yannh/kubeconform/blob/master/scripts/openapi2jsonschema.py)
> script.

### Kubernetes APIs

```shell
flux schema extract k8s [swagger-file]
```

Reads a Kubernetes OpenAPI v2 swagger document and emits one file per kind
listed under `x-kubernetes-group-version-kind`. Helper types (e.g. `PodSpec`,
`ObjectMeta`) are inlined into the kinds that reference them, not written as
standalone files.

| Flag                  | Description                                                                                                                   |
|-----------------------|-------------------------------------------------------------------------------------------------------------------------------|
| `--version X.Y.Z`     | Fetch the swagger from `github.com/kubernetes/kubernetes` for the given release tag (mutually exclusive with a swagger file). |
| `-d, --output-dir`    | Directory to write JSON Schema files to.                                                                                      |
| `-f, --output-format` | Go template for output file paths (default: `{{ .Group }}/{{ .Kind }}_{{ .Version }}.json`).                                  |
| `--index-source`          | Source name and version recorded in field index headers, overriding auto-detection.                                   |
| `--strip-description`     | Drop `description` fields from the generated schemas to reduce their size.                                            |
| `--with-field-index`      | Also write a `.fields.txt` field index next to each schema.                                                           |
| `--with-explain-metadata` | Include `x-flux-schema-*` annotations, alias redirects, and `.explain/` lookup shards used by `flux schema explain`.                                          |

Pin the catalog to a specific Kubernetes release:

```shell
flux schema extract k8s --version 1.35.0 -d ./my-catalog
```

Or extract from a live cluster:

```shell
kubectl get --raw /openapi/v2 | flux schema extract k8s -d ./my-catalog
```

Core API kinds (`apiVersion: v1`) render under the `core/` group directory.

### OpenShift APIs

```shell
flux schema extract openshift [swagger-file]
```

Reads an `openshift/api` OpenAPI v2 swagger document and emits one file per
OpenShift resource. Only definitions in the `openshift` namespace are
emitted; embedded upstream Kubernetes types (e.g. `Pod`) are inlined.

| Flag                  | Description                                                                                                                           |
|-----------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| `--ref REF`           | Fetch the swagger from `github.com/openshift/api` at the given git ref (e.g. `release-4.20`); mutually exclusive with a swagger file. |
| `-d, --output-dir`    | Directory to write JSON Schema files to.                                                                                              |
| `-f, --output-format` | Go template for output file paths (default: `{{ .Group }}/{{ .Kind }}_{{ .Version }}.json`).                                          |
| `--index-source`          | Source name and version recorded in field index headers, overriding auto-detection.                                           |
| `--strip-description`     | Drop `description` fields from the generated schemas to reduce their size.                                                    |
| `--with-field-index`      | Also write a `.fields.txt` field index next to each schema.                                                                   |
| `--with-explain-metadata` | Include `x-flux-schema-*` annotations, alias redirects, and `.explain/` lookup shards used by `flux schema explain`.                                                  |

Pin to an OpenShift release branch:

```shell
flux schema extract openshift --ref release-4.20 -d ./my-catalog
```

Or feed in a downloaded swagger:

```shell
curl -sL https://raw.githubusercontent.com/openshift/api/release-4.20/openapi/openapi.json | \
  flux schema extract openshift -d ./my-catalog
```

### A typical refresh script

A single script can populate a catalog with everything you need — the
default `-f` template means the outputs land side by side under the same
root and resolve to one `--schema-location`:

```shell
#!/usr/bin/env bash
set -euo pipefail

CATALOG=./my-catalog
mkdir -p "$CATALOG"

# Kubernetes core + built-ins
flux schema extract k8s --version 1.35.0 --strip-description -d "$CATALOG"

# Vendor CRDs at a pinned version
kubectl kustomize \
  https://github.com/external-secrets/external-secrets/config/crds/bases?ref=v2.3.0 | \
  flux schema extract crd --strip-description -d "$CATALOG"
```

Pass `--strip-description` to every generator to keep the catalog small —
the official catalog uses it everywhere and gets about a 54% size
reduction on native Kubernetes schemas.

The official catalog's generator scripts under
[`scripts/`](../scripts/) are a working reference: `gen-k8s-schemas.sh`,
`gen-flux-schemas.sh`, `gen-crd-schemas.sh`, and `gen-openshift-schemas.sh`.

## Schema output

All `extract` commands emit the standalone-strict JSON Schema variant, so
schemas in the catalog are usable on their own and reproduce the
constraints the Kubernetes API server enforces:

- Every `$ref` is inlined, so each schema has no cross-file dependencies.
- Objects with `properties` are closed with `additionalProperties: false`,
  except under nodes marked `x-kubernetes-preserve-unknown-fields: true`,
  which stay open so free-form maps validate correctly.
- Integer-or-string fields are rewritten to
  `oneOf: [{type: string}, {type: integer}]`. Both the legacy
  `format: int-or-string` and the structural
  `x-kubernetes-int-or-string: true` forms are recognized.
- Optional fields are marked nullable (`type: [<t>, "null"]`), matching the
  API server's behavior of accepting `null` for unset optional values.
- `apiVersion` and `kind` are injected into every kind's properties and
  required list.
- `x-flux-schema-*` explain annotations, resource alias redirects, and the
  sharded `.explain/` metadata tree are omitted by default. Pass
  `--with-explain-metadata` only for catalogs that need exact
  `flux schema explain` kind names, short names, plural names, full resource
  names, type-reference shell completion, and field type names.

## Field indexes

Pass `--with-field-index` to the `extract` commands to write greppable,
LLM-friendly field indexes alongside the extracted schemas: one
`.fields.txt` file per schema, with one self-contained line per field.
See the [field index reference](field-index.md) for the file naming, line
grammar, and annotations.

## Hosting

A catalog is just a tree of files. The validator's loader fetches each
rendered location over `http`/`https` or from the local filesystem.
Common options:

- **Filesystem**: vendor the catalog into the same repo as your manifests, or
  publish it as a release artifact / OCI artifact and unpack it in CI.
- **HTTP(S)**: serve the catalog from any static host (GitHub raw, S3,
  GitHub Pages, etc.) and pass the base URL to `--schema-location`. The
  loader expands bare URLs with the default template.

A 404 or `ENOENT` for a given `apiVersion`/`kind` falls through to the next
`--schema-location`, so you can layer your catalog on top of the built-in
one:

```shell
flux schema validate ./manifests \
  --schema-location ./my-catalog \
  --schema-location default
```

Order matters — the first match wins, so put your overrides before `default`.

## Refreshing

Custom catalogs go stale as upstream versions move. Schedule the refresh
script in CI (cron workflow, scheduled pipeline, etc.) and commit the result
back, the same way the official catalog is updated by
[`.github/workflows/update-catalog.yaml`](../.github/workflows/update-catalog.yaml).
