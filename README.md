# flux-schema

[![release](https://img.shields.io/github/release/fluxcd/flux-schema/all.svg)](https://github.com/fluxcd/flux-schema/releases)
[![test](https://github.com/fluxcd/flux-schema/actions/workflows/test.yaml/badge.svg)](https://github.com/fluxcd/flux-schema/actions/workflows/test.yaml)
[![cve-scan](https://github.com/fluxcd/flux-schema/workflows/cve-scan/badge.svg)](https://github.com/fluxcd/flux-schema/actions/workflows/cve-scan.yml)
[![license](https://img.shields.io/github/license/fluxcd/flux-schema.svg)](https://github.com/fluxcd/flux-schema/blob/main/LICENSE)
[![slsa](https://slsa.dev/images/gh-badge-level2.svg)](https://github.com/fluxcd/flux-schema/attestations)

Flux CLI plugin for Kubernetes schema extraction and manifests validation.

> [!NOTE]
> This repository is in early development and the plugin system is not yet available in a stable release of Flux.
> The instructions for installing and using the plugin will be added here when [RFC-0013](https://github.com/fluxcd/flux2/blob/main/rfcs/0013-cli-plugin-system/README.md)
> has been implemented and released in Flux 2.9 or later.

## Available Commands

- `flux-schema extract [crds.yaml]`: Extract JSON Schema from Kubernetes CRD YAMLs
  - `-d, --output-dir`: Directory to write JSON Schema files to (mutually exclusive with `--output-archive`)
  - `-a, --output-archive`: Path to write a gzipped tar archive of JSON Schema files to
  - `-f, --output-format`: Go template for output file paths (default: `{{ .Kind }}-{{ .GroupPrefix }}-{{ .Version }}.json`)

### JSON Schema Extraction

The extract command reads Kubernetes CustomResourceDefinition YAML and writes one JSON Schema file per CRD version.
The input can be a bare CRD, a `List` of CRDs, or a multi-document YAML stream.

Generate schemas for every CRD installed in a cluster:

```shell
kubectl get crds -o yaml > crds.yaml
flux-schema extract crds.yaml -d ./schemas
```

> The output is compatible with `kubeconform` and `kubeval`, making this command a drop-in replacement for kubeconform's
> [openapi2jsonschema.py](https://github.com/yannh/kubeconform/blob/master/scripts/openapi2jsonschema.py) script.

You can supply `-f, --output-format` with a Go template to change the layout, e.g. the
[CRDs-catalog](https://github.com/datreeio/CRDs-catalog) per-group-directory layout:

```shell
flux-schema extract crds.yaml -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json'
```

Nested directories referenced by `-f, --output-format` are created automatically.

To bundle the schemas into a gzipped tar archive instead of writing to a directory,
use `-a, --output-archive`:

```shell
kustomize build config/crd | flux-schema extract /dev/stdin -a dist/crd-schemas.tar.gz
```

The archive path must end in `.tar.gz` or `.tgz`, and its parent directory is created if missing.
`-f, --output-format` still controls the entry paths inside the archive, so a nested template produces
subdirectories within the tarball.

Supported template variables (all lowercased at render time):

| Variable       | Example                    |
|----------------|----------------------------|
| `.Group`       | `source.toolkit.fluxcd.io` |
| `.GroupPrefix` | `source`                   |
| `.Kind`        | `gitrepository`            |
| `.Version`     | `v1`                       |

Note that the generated schemas apply the following OpenAPI → JSON Schema transformations:

- Objects with `properties` are closed with `additionalProperties: false`, except under nodes
  marked with `x-kubernetes-preserve-unknown-fields: true`, which stay open so free-form maps validate correctly.
- Integer-or-string fields are rewritten to `oneOf: [{type: string}, {type: integer}]`. Both the
  legacy `format: int-or-string` and the structural `x-kubernetes-int-or-string: true` forms are recognized.
