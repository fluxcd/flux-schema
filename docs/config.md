---
linkTitle: Configuration
weight: 10
---

# Flux Schema Config

The `flux schema validate` and `flux schema explain` commands can load default
flag values from a YAML configuration file. The file shape is versioned and
documented by the JSON Schema in [`config-v1beta1.json`](config-v1beta1.json).

## Example

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schemaLocation:
    - ecosystem
  skipKind:
    - source.toolkit.fluxcd.io/v1/ExternalArtifact
  skipJSONPath:
    - Secret:/sops
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
explain:
  schemaLocation:
    - ecosystem
  apiVersion: v1
  recursive: false
  insecureSkipTLSVerify: false
  output: plaintext
```

Usage:

```shell
flux schema validate ./manifests --config .fluxschema.yml
flux schema explain pods.spec --config .fluxschema.yml
```

When `--config` is not set, the `FLUX_SCHEMA_CONFIG` environment variable is
used. When neither is set, `validate` uses the executable-adjacent config file
when it exists. `explain` reads that same path when no `--schema-location` is
passed, for example `~/.fluxcd/plugins/flux-schema.config`.

## Specification

| Field                               | Description                                                      |
|-------------------------------------|------------------------------------------------------------------|
| `apiVersion`                        | Config API version. Currently `schema.plugin.fluxcd.io/v1beta1`. |
| `kind`                              | Config API kind. Currently `Config`.                             |
| `validate`                          | Defaults for validation options.                                 |
| `explain`                           | Defaults for explain options.                                    |

### Validation

The `validate` section configures defaults for the `flux schema validate` flags.

| Field                   | Description                                                                                                                                                               |
|-------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `schemaLocation[]`      | Schema URLs, file paths, or templates tried in order. Aliases: `default` (built-in catalog), `ecosystem` ([schemas.fluxoperator.dev](https://schemas.fluxoperator.dev/)). |
| `skipMissingSchemas`    | Skip documents for which no schema can be found.                                                                                                                          |
| `skipKind[]`            | Kind or apiVersion/kind patterns excluded from validation.                                                                                                                |
| `skipJSONPath[]`        | JSON Pointers stripped before validation.                                                                                                                                 |
| `skipFile[]`            | Basename glob patterns excluded from validation.                                                                                                                          |
| `skipCELRules`          | Disable evaluation of `x-kubernetes-validations` CEL rules.                                                                                                               |
| `verbose`               | Print a line for every document, including valid and skipped.                                                                                                             |
| `failFast`              | Exit after the first invalid document.                                                                                                                                    |
| `concurrent`            | Number of concurrent validation workers.                                                                                                                                  |
| `insecureSkipTLSVerify` | Disable TLS certificate verification when downloading schemas.                                                                                                            |
| `output`                | Output format: `text`, `json`, or `yaml`.                                                                                                                                 |

When the `output` field is set to `json` or `yaml`, the result has the [Report API](report.md) shape.

### Explain

The `explain` section configures defaults for the `flux schema explain` flags.

| Field                   | Description                                                    |
|-------------------------|----------------------------------------------------------------|
| `schemaLocation[]`      | Schema URLs, file paths, or templates tried in order. Aliases: `default` (built-in catalog), `ecosystem` ([schemas.fluxoperator.dev](https://schemas.fluxoperator.dev/)). |
| `apiVersion`            | API group/version to explain by default.                       |
| `recursive`             | Print fields of fields.                                        |
| `insecureSkipTLSVerify` | Disable TLS certificate verification when downloading schemas. |
| `output`                | Output format: `plaintext` or `plaintext-openapiv2`.           |
