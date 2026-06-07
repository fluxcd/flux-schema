# Flux Schema Config

The `flux-schema validate` command can load default flag values from a YAML
configuration file. The file shape is versioned and documented by the JSON
Schema in [`config-v1beta1.json`](config-v1beta1.json).

## Example

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schema-location:
    - default
    - https://raw.githubusercontent.com/datreeio/CRDs-catalog/main
  skip-kind:
    - source.toolkit.fluxcd.io/v1/ExternalArtifact
  skip-json-path:
    - Secret:/sops
  skip-file:
    - '.*'
    - kustomization.yaml
  skip-cel-rules: false
  skip-missing-schemas: false
  verbose: true
  fail-fast: false
  concurrent: 8
  insecure-skip-tls-verify: false
  output: text
```

Usage:

```shell
flux-schema validate ./manifests --config .fluxschema.yml
```

The `FLUX_SCHEMA_CONFIG` environment variable can also point at the config
file. The `--config` flag wins when both are set.

## Specification

| Field                               | Description                                                      |
|-------------------------------------|------------------------------------------------------------------|
| `apiVersion`                        | Config API version. Currently `schema.plugin.fluxcd.io/v1beta1`. |
| `kind`                              | Config API kind. Currently `Config`.                             |
| `validate`                          | Defaults for validation options.                                 |

### Validation

The `validate` section mirrors the `flux-schema validate` flags.

| Field                      | Description                                                    |
|----------------------------|----------------------------------------------------------------|
| `schema-location[]`        | Schema URLs, file paths, or templates tried in order.          |
| `skip-missing-schemas`     | Skip documents for which no schema can be found.               |
| `skip-kind[]`              | Kind or apiVersion/kind patterns excluded from validation.     |
| `skip-json-path[]`         | JSON Pointers stripped before validation.                      |
| `skip-file[]`              | Basename glob patterns excluded from validation.               |
| `skip-cel-rules`           | Disable evaluation of `x-kubernetes-validations` CEL rules.    |
| `verbose`                  | Print a line for every document, including valid and skipped.  |
| `fail-fast`                | Exit after the first invalid document.                         |
| `concurrent`               | Number of concurrent validation workers.                       |
| `insecure-skip-tls-verify` | Disable TLS certificate verification when downloading schemas. |
| `output`                   | Output format: `text`, `json`, or `yaml`.                      |

When the `output` field is set to `json` or `yaml`, the result has the [Report API](../report/README.md) shape.
