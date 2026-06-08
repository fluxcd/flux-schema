# Flux Schema Config

The `flux-schema validate` command can load default flag values from a YAML
configuration file. The file shape is versioned and documented by the JSON
Schema in [`config-v1beta1.json`](config-v1beta1.json).

## Example

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schemaLocation:
    - default
    - https://raw.githubusercontent.com/datreeio/CRDs-catalog/main
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
```

Usage:

```shell
flux-schema validate ./manifests --config .fluxschema.yml
```

When `--config` is not set, the `FLUX_SCHEMA_CONFIG` environment variable is
used. When neither is set, `flux-schema` looks for a config file at the
executable path plus `.config`, for example `~/.fluxcd/plugins/flux-schema.config`.

## Specification

| Field                               | Description                                                      |
|-------------------------------------|------------------------------------------------------------------|
| `apiVersion`                        | Config API version. Currently `schema.plugin.fluxcd.io/v1beta1`. |
| `kind`                              | Config API kind. Currently `Config`.                             |
| `validate`                          | Defaults for validation options.                                 |

### Validation

The `validate` section configures defaults for the `flux-schema validate` flags.

| Field                      | Description                                                    |
|----------------------------|----------------------------------------------------------------|
| `schemaLocation[]`         | Schema URLs, file paths, or templates tried in order.          |
| `skipMissingSchemas`       | Skip documents for which no schema can be found.               |
| `skipKind[]`               | Kind or apiVersion/kind patterns excluded from validation.     |
| `skipJSONPath[]`           | JSON Pointers stripped before validation.                      |
| `skipFile[]`               | Basename glob patterns excluded from validation.               |
| `skipCELRules`             | Disable evaluation of `x-kubernetes-validations` CEL rules.    |
| `verbose`                  | Print a line for every document, including valid and skipped.  |
| `failFast`                 | Exit after the first invalid document.                         |
| `concurrent`               | Number of concurrent validation workers.                       |
| `insecureSkipTLSVerify`    | Disable TLS certificate verification when downloading schemas. |
| `output`                   | Output format: `text`, `json`, or `yaml`.                      |

When the `output` field is set to `json` or `yaml`, the result has the [Report API](../report/README.md) shape.
