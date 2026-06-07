# flux-schema validation report

The `flux-schema validate` command can emit a structured report of the
validation results by setting `--output` to `json` or `yaml`. The envelope
shape is versioned and documented by the JSON Schema in
[`report-v1beta1.json`](./report-v1beta1.json).
The legacy `version: "1.0.0"` envelope schema remains available at
[`schema-1.0.0.json`](./schema-1.0.0.json).

## Usage

```shell
flux-schema validate ./manifests -o json
```

Structured output always emits every result regardless of `--verbose`.
Filtering belongs downstream (`jq`, `yq`). The process exit code still
reflects whether any document was invalid.

## Envelope

Every report is wrapped in a top-level envelope:

| Key                | Description                                               |
|--------------------|-----------------------------------------------------------|
| `apiVersion`       | Report API version. Currently `schema.plugin.fluxcd.io/v1beta1`. |
| `kind`             | Report API kind. Currently `Report`.                      |
| `$schema`          | URL of the JSON Schema describing the envelope.           |
| `report.reporter`  | Identity of the producer, e.g. `flux-schema/0.1.0`.       |
| `report.timestamp` | RFC 3339 UTC timestamp of the run.                        |
| `report.summary`   | Aggregate counts: `total`, `valid`, `invalid`, `skipped`. |
| `report.results[]` | One entry per validated document, in source order.        |

## Result fields

| Key            | Description                                                                                                                                                               |
|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `resource`     | `{apiVersion, kind, namespace?, name?}` or `null` when no Kubernetes identity could be recovered (e.g. a file that fails to open, or stdin that is not YAML).             |
| `source`       | File path or `stdin`.                                                                                                                                                     |
| `idx`          | 1-based position of the document within its source. `0` for source-level failures that have no document.                                                                  |
| `status`       | `"valid"`, `"invalid"`, or `"skipped"`.                                                                                                                                   |
| `reason`       | Stable kebab-case code (see below). Omitted when `status` is `"valid"`.                                                                                                   |
| `violations[]` | Zero or more `{path?, message}` entries. `path` is a JSON Pointer set by schema violations; for every other reason it is absent and `message` carries the raw error text. |

## Reasons

| Reason              | Triggered by                                                                                                                                           |
|---------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| `source-load-error` | Source-level open/read failure.                                                                                                                        |
| `yaml-parse-error`  | Strict YAML decode fails (duplicate keys, malformed doc).                                                                                              |
| `schema-load-error` | Schema loader failure (HTTP fetch, file read, or JSON Schema compile).                                                                                 |
| `schema-not-found`  | No schema applicable — either no schema file matches the GVK, or the document has no GVK to look up.                                                   |
| `schema-violation`  | Document fails one or more schema constraints. `violations[]` carries a JSON Pointer `path` per entry.                                                 |
| `cel-violation`     | Document fails one or more `x-kubernetes-validations` CEL rules, or the schema's CEL evaluator could not be built. JSON Schema constraints all passed. |
| `kind-skipped`      | Matched a `--skip-kind` pattern.                                                                                                                       |

## Example

```json
{
  "apiVersion": "schema.plugin.fluxcd.io/v1beta1",
  "kind": "Report",
  "$schema": "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/report/report-v1beta1.json",
  "report": {
    "reporter": "flux-schema/v0.1.0",
    "timestamp": "2026-06-01T12:00:00Z",
    "summary": {
      "total": 7,
      "valid": 1,
      "invalid": 5,
      "skipped": 1
    },
    "results": [
      {
        "resource": {
          "apiVersion": "apps/v1",
          "kind": "Deployment",
          "namespace": "default",
          "name": "web"
        },
        "source": "manifests/app.yaml",
        "idx": 1,
        "status": "valid"
      },
      {
        "resource": {
          "apiVersion": "source.toolkit.fluxcd.io/v1",
          "kind": "Bucket",
          "namespace": "apps",
          "name": "minio"
        },
        "source": "manifests/sources.yaml",
        "idx": 1,
        "status": "invalid",
        "reason": "schema-violation",
        "violations": [
          {
            "path": "/spec",
            "message": "missing property 'bucketName'"
          },
          {
            "path": "/spec/interval",
            "message": "got number, want string"
          },
          {
            "path": "/spec",
            "message": "additional properties 'force' not allowed"
          }
        ]
      },
      {
        "resource": {
          "apiVersion": "source.toolkit.fluxcd.io/v1",
          "kind": "OCIRepository",
          "namespace": "default",
          "name": "podinfo"
        },
        "source": "manifests/sources.yaml",
        "idx": 2,
        "status": "invalid",
        "reason": "yaml-parse-error",
        "violations": [
          {
            "message": "line 18: key \"app.kubernetes.io/name\" already set in map"
          }
        ]
      },
      {
        "resource": null,
        "source": "manifests/missing.yaml",
        "idx": 0,
        "status": "invalid",
        "reason": "source-load-error",
        "violations": [
          {
            "message": "open manifests/missing.yaml: no such file or directory"
          }
        ]
      },
      {
        "resource": {
          "apiVersion": "example.com/v1",
          "kind": "Widget",
          "namespace": "default",
          "name": "w1"
        },
        "source": "manifests/widgets.yaml",
        "idx": 1,
        "status": "invalid",
        "reason": "schema-load-error",
        "violations": [
          {
            "message": "Get \"https://schemas.example.com/Widget_v1.json\": dial tcp: lookup schemas.example.com: no such host"
          }
        ]
      },
      {
        "resource": {
          "apiVersion": "artifact.toolkit.fluxcd.io/v1",
          "kind": "ArtifactGenerator",
          "namespace": "apps",
          "name": "podinfo"
        },
        "source": "manifests/artifacts.yaml",
        "idx": 1,
        "status": "invalid",
        "reason": "schema-not-found",
        "violations": [
          {
            "message": "no schema for kind \"ArtifactGenerator\" in version \"artifact.toolkit.fluxcd.io/v1\""
          }
        ]
      },
      {
        "resource": {
          "apiVersion": "v1",
          "kind": "Secret",
          "namespace": "apps",
          "name": "auth-sops"
        },
        "source": "manifests/secrets.yaml",
        "idx": 1,
        "status": "skipped",
        "reason": "kind-skipped"
      }
    ]
  }
}
```
