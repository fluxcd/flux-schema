---
weight: 20
---

# Kubernetes Manifests Validation with Flux Schema CLI

The `flux schema validate` command validates Kubernetes YAML manifests from one
or more files or directories against a JSON Schema resolved from each
document's `apiVersion` and `kind`.

Examples:

```shell
# Validate plain YAML files and build kustomize directories in a tree
flux schema validate ./manifests --skip-missing-schemas

# Validate a Helm chart by piping the rendered output
helm template ./charts/app | flux schema validate --verbose
```

A non-zero exit code is returned when any document is invalid or errored.

## Kustomize build

A directory that contains `kustomization.yaml`, `kustomization.yml`, or
`Kustomization` is built with kustomize, like kustomize-controller does, and
the rendered resources are validated instead of the individual files.
A kustomization file passed as an argument builds its directory:

```shell
flux schema validate ./clusters/production --verbose
```

A failed build produces one invalid result with the reason `kustomize-build-error`.
This includes a `kind: Component` directory, because kustomize can't build a component
on its own. Exclude such directories with `--skip-file`.

The build uses the same options as
`kustomize build --load-restrictor=LoadRestrictionsNone`:

- Files outside the built directory can be referenced e.g. `../base`.
- Remote bases are fetched. Git-backed references need `git` in `PATH`.
- Builtin generators and transformers run. Exec and KRM function plugins are disabled.

Results of a build use the root kustomization file as `source`. When a
resource comes from a file, `origin` holds its path, joined with the built
directory. Remote files use the form `<repo>//<path>?ref=<ref>`.

## Flags

| Flag                         | Description                                                                                                                                     |
|------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------|
| `-s, --schema-location`      | URL or file path for schemas (repeatable, tried in order); `default` points at the built-in catalog, `ecosystem` at the CNCF ecosystem catalog. |
| `--skip-missing-schemas`     | Skip documents for which no schema can be found.                                                                                                |
| `--skip-kind`                | Skip documents matching `kind` or `apiVersion/kind` (repeatable).                                                                               |
| `--skip-json-path`           | Strip a JSON Pointer field before validation, optionally scoped: `[apiVersion/kind:]/path` (repeatable).                                        |
| `--skip-json-path-if-absent` | Skip missing required-field errors for a JSON Pointer field, optionally scoped: `[apiVersion/kind:]/path` (repeatable).                         |
| `--skip-file`                | Basename glob for files and dirs in the walk; skipping a kustomization file restores per-file validation (repeatable, default: `.*`).           |
| `--skip-cel-rules`           | Skip evaluation of `x-kubernetes-validations` CEL rules.                                                                                        |
| `--fail-fast`                | Exit after the first invalid document.                                                                                                          |
| `--concurrent`               | Number of concurrent workers (default 8).                                                                                                       |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification when fetching schemas over HTTPS.                                                                          |
| `-v, --verbose`              | Print a line for every document, including valid and skipped ones.                                                                              |
| `-o, --output`               | Output format, one of `text`, `json`, `yaml` or `junit` (default: `text`).                                                                      |                                                                          |                                                   |
| `-d, --output-dir`           | Output directory where structured report files are written (created if missing).                                                                |                                                                          |                                                   |
| `--config`                   | Path to a YAML file supplying default values for validate flags (env: `FLUX_SCHEMA_CONFIG`).                                                    |

## Schema location

When no `--schema-location` is given, `validate` uses the
[Flux Schema catalog](../catalog/README.md), which covers the latest Kubernetes and OpenShift APIs,
the stable channel of Gateway API, and the Flux ecosystem CRDs:

```shell
flux schema validate ./manifests
```

To validate against your own schemas generated by `flux schema extract`, pass
the directory as `--schema-location`. Bare paths and URLs are auto-expanded to
the catalog layout `{{.Group}}/{{.Kind}}_{{.Version}}.json`:

```shell
flux schema validate ./manifests --schema-location ./my-schemas
```

For a different layout, pass a full Go template ending in `.json`:

```shell
flux schema validate ./manifests \
  --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'
```

Template variables are `.Group`, `.GroupPrefix`, `.Kind`, and `.Version`.

The `--schema-location` flag is repeatable and locations are tried in order
(the first match wins). Pass the literal value `default` to include the
Flux Schema catalog alongside your own schemas:

```shell
flux schema validate ./manifests \
  --schema-location default \
  --schema-location ecosystem \
  --schema-location './schemas/{{.Kind}}-{{.GroupPrefix}}-{{.Version}}.json'
```

The literal value `ecosystem` resolves to the
[CNCF ecosystem catalog](https://schemas.fluxoperator.dev) served from
`https://schemas.fluxoperator.dev/catalog/`, covering CRDs of hundreds of CNCF
projects and rebuilt daily from upstream releases.

See [custom catalogs](custom-schema-catalog.md) for guidance on building and hosting
your own schema catalog.

During validation, resource-root `metadata` is treated as Kubernetes
`ObjectMeta`. If a compact CRD schema declares only part of
`metadata.properties`, Flux Schema completes the loaded schema in memory so
standard fields like `namespace`, `labels`, and `annotations` do not fail as
additional properties. Existing schema constraints on fields such as
`metadata.name` and `metadata.generateName` are preserved. CEL rules still use
Kubernetes' structural schema view of root metadata, where only `name` and
`generateName` are implicitly visible.

## Skipping documents and fields

Manifests can be piped in and certain documents skipped with `--skip-kind`:

```shell
kustomize build . | flux schema validate \
  --skip-kind 'Service' \ # matches any Service kind regardless of apiVersion
  --skip-kind 'source.toolkit.fluxcd.io/v1/ExternalArtifact'
```

Some manifests carry tooling-injected fields that are stripped at apply time
by Flux (e.g. SOPS-encrypted Secrets). Use `--skip-json-path` to remove those
fields from validation so the rest of the document is still checked:

```shell
flux schema validate ./manifests \
  --skip-json-path 'v1/Secret:/sops' \
  --skip-json-path 'Deployment:/sops'
```

If admission hooks such as Kyverno or `MutatingAdmissionPolicy` add default
values before API server validation, use `--skip-json-path-if-absent` for
required fields that may be omitted from Git. Only the missing-field error is
skipped; if the field is present, its value is still validated against the
schema and CEL rules:

```shell
flux schema validate ./manifests \
  --skip-json-path-if-absent 'Widget:/spec/replicas'
```

When a matching path is absent, CEL evaluation is skipped for that document
because Flux Schema cannot apply out-of-band admission defaults before running
CEL.

## Output

Default output (`-o text`) prints one line per document with its validation result,
and a summary at the end. To print valid documents and skipped ones alongside
invalid ones, pass `--verbose`:

```console
$ flux schema validate ./manifests --verbose

manifests/releases.yaml - helm.toolkit.fluxcd.io/v2/HelmRelease/apps/frontend is invalid: cel violation
  - /spec: Invalid value: either 'chart' or 'chartRef' must be set
manifests/sources.yaml - source.toolkit.fluxcd.io/v1/Bucket/apps/frontend-config is invalid: schema violation
  - /spec: missing property 'bucketName'
  - /spec/interval: got number, want string
  - /spec/secretRef/name: got object, want string
  - /spec: additional properties 'force' not allowed
manifests/sources.yaml - source.toolkit.fluxcd.io/v1/OCIRepository/apps/frontend is invalid: yaml parse error
  - line 10: key "app.kubernetes.io/name" already set in map
manifests/sources.yaml - source.toolkit.fluxcd.io/v1/HelmChart/apps/frontend is valid
manifests/sources.yaml - v1/Secret/apps/auth-sops is skipped: kind skipped
Summary: 5 resources found in 2 files - Valid: 1, Invalid: 3, Skipped: 1
```

For CI pipelines and tooling, use `-o` to select a structured output format:
`json`, `yaml`, or `junit`:

```shell
flux schema validate ./manifests -o json
```

The `json` and `yaml` formats emit the structured validation report described
by [`report-v1beta1.json`](report-v1beta1.json).
The `junit` format emits [JUnit XML](junit.md), which can be consumed by CI
systems to present validation failures as test results, making them easier to
inspect directly in the CI interface.

Structured reports may also be emitted as files into a directory by using the
`-d` flag: `-d /report-dir`. The directory will be created if missing.

Example JSON output:

```json
{
  "apiVersion": "schema.plugin.fluxcd.io/v1beta1",
  "kind": "Report",
  "$schema": "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/report-v1beta1.json",
  "report": {
    "reporter": "flux-schema/v0.1.0",
    "timestamp": "2026-05-20T12:00:00Z",
    "summary": {
      "total": 3,
      "valid": 1,
      "invalid": 2,
      "skipped": 0
    },
    "results": [
      {
        "resource": {
          "apiVersion": "v1",
          "kind": "Namespace",
          "name": "apps"
        },
        "source": "apps/staging/kustomization.yaml",
        "origin": "apps/base/namespace.yaml",
        "idx": 1,
        "status": "valid"
      },
      {
        "resource": {
          "apiVersion": "helm.toolkit.fluxcd.io/v2",
          "kind": "HelmRelease",
          "name": "frontend",
          "namespace": "apps"
        },
        "source": "apps/staging/kustomization.yaml",
        "origin": "apps/base/release.yaml",
        "idx": 2,
        "status": "invalid",
        "reason": "cel-violation",
        "violations": [
          {
            "path": "/spec",
            "message": "Invalid value: either 'chart' or 'chartRef' must be set"
          }
        ]
      },
      {
        "resource": {
          "apiVersion": "source.toolkit.fluxcd.io/v1",
          "kind": "OCIRepository",
          "name": "frontend",
          "namespace": "apps"
        },
        "source": "apps/staging/kustomization.yaml",
        "origin": "apps/base/repository.yaml",
        "idx": 3,
        "status": "invalid",
        "reason": "schema-violation",
        "violations": [
          {
            "path": "/spec",
            "message": "additional properties 'force' not allowed"
          },
          {
            "path": "/spec/interval",
            "message": "got number, want string"
          }
        ]
      }
    ]
  }
}
```

See the [validation report reference](report.md) for the full envelope
shape, the `reason` enum, and an example covering every result type. The
report is versioned by a published [JSON Schema](report-v1beta1.json).

## Validation rules

- YAML documents with duplicate keys are rejected matching Flux behavior.
- Documents missing both `metadata.name` and `metadata.generateName` are
  flagged as invalid matching Kubernetes API behavior.
- `metadata.name`, `generateName`, `namespace`, and `labels`/`annotations`
  keys and values are checked against the Kubernetes API server's ObjectMeta
  rules (DNS-1123, qualified names).
- Schemas produced by `flux schema extract crd` close objects with
  `additionalProperties: false`, so undocumented fields under `spec` fail
  validation.
- Kubernetes admission extensions are enforced for embedded resources and list
  topology: `x-kubernetes-embedded-resource` validates nested `apiVersion`,
  `kind`, and `metadata`, while `x-kubernetes-list-type: map|set` rejects
  duplicate list entries using `x-kubernetes-list-map-keys` for map lists.
- String formats `duration`, `date`, `datetime`/`date-time`, and `time` are
  validated matching Kubernetes OpenAPI conventions.

## CEL validation rules

By default, the validate command extracts the `x-kubernetes-validations`
rules from the schemas and evaluates them as CEL expressions using the same
engine as the Kubernetes API.

CEL evaluation runs only after JSON Schema validation passes, and any rule
violations are reported with the `cel-violation` reason. Before evaluating
rules, flux-schema applies the schema's structural defaults in memory, matching
kube-apiserver ordering. Transition rules referencing `oldSelf` evaluate with
no prior state, matching the Kubernetes API behavior on `CREATE`.

The CEL validation can be disabled with the `--skip-cel-rules` flag.

## Config file

Flag values can be pre-set in a YAML config file and referenced with`--config`
or with the `FLUX_SCHEMA_CONFIG` environment variable. This keeps long invocations
out of CI scripts and makes validation reproducible across environments.

```shell
flux schema validate ./manifests --config .fluxschema.yml
```

The file is wrapped in a `Config` API envelope and has a `validate` section
for validation defaults. The shape is documented by the
[Config JSON Schema](config-v1beta1.json).

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schemaLocation:
    - default
    - ecosystem
  skipKind:
    - source.toolkit.fluxcd.io/v1/ExternalArtifact
  skipJSONPath:
    - Secret:/sops
  skipJSONPathIfAbsent:
    - Widget:/spec/replicas
  skipFile:
    - '.*'
    - kustomization.yaml
  skipCELRules: false
  skipMissingSchemas: false
  verbose: true
  failFast: false
  concurrent: 8
  insecureSkipTLSVerify: false
  output: text
```

Rules:

- CLI flags override config values. Setting `--verbose=false` wins over
  `verbose: true` in the file.
- Setting `--config` overrides `FLUX_SCHEMA_CONFIG`. When both are set, the
  flag wins and the env var is ignored.
- Manifest paths stay positional. The config file configures how to validate;
  paths are given on the command line.

## Running in CI

### GitHub Actions

See the [validate action](../actions/validate/README.md) reference.

### Docker

The `ghcr.io/fluxcd/flux-schema` image bakes the [default catalog](../catalog/README.md)
into `/catalog/latest/`, so manifests can be validated offline without fetching schemas from GitHub.

Mount the manifests directory and point `--schema-location` at the builtin catalog:

```sh
docker run --rm \
  -v "$PWD/manifests:/manifests:ro" \
  ghcr.io/fluxcd/flux-schema:latest validate /manifests \
  --schema-location /catalog/latest
```

Or pipe rendered manifests into the container over stdin
(note the `-i` flag to keep stdin open):

```sh
kustomize build ./manifests | docker run --rm -i \
  ghcr.io/fluxcd/flux-schema:latest validate \
  --schema-location /catalog/latest \
  --skip-missing-schemas
```

Notes:

- Use `--schema-location /catalog/latest` rather than `default` for air-gapped environments.
- Pin the image to a release tag e.g. `ghcr.io/fluxcd/flux-schema:v0.3.0`.
- Tu use your own schema catalog, repeat `--schema-location` with another mounted directory.
