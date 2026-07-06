---
weight: 25
---

# Kubernetes Schema Explain with Flux Schema CLI

The `flux schema explain` command prints kubectl-style field documentation from
JSON Schema catalogs, without contacting a Kubernetes API server.

Examples:

```shell
# Explain a native Kubernetes resource from a local catalog
flux schema explain pods --api-version=v1 --schema-location ./catalog

# Explain a Flux resource from a description-preserving remote catalog
flux schema explain hr.spec \
  --schema-location https://raw.githubusercontent.com/controlplaneio-fluxcd/schema-catalog/main/catalog

# Explain a nested field
flux schema explain pods.spec.containers --api-version=v1 --schema-location ./catalog

# Print nested fields recursively
flux schema explain pods --api-version=v1 --recursive --schema-location ./catalog
```

## Flags

| Flag | Description |
|------|-------------|
| `--schema-location` | URL or file path for schemas (repeatable); `default` points at the built-in validation catalog. |
| `-f, --config` | YAML config file with `explain` defaults; defaults to `$FLUX_SCHEMA_CONFIG`, else `<executable>.config`. |
| `--api-version` | Get different explanations for a particular API version (`group/version`). |
| `--recursive` | Print fields of fields. |
| `-o, --output` | Output format, one of `plaintext` or `plaintext-openapiv2` (default: `plaintext`). |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification when fetching schemas over HTTPS. |

When `--schema-location` is not passed, `explain` reads config from
`--config`, then `$FLUX_SCHEMA_CONFIG`, then a file next to the running binary
named `<binary>.config`. That config must set `explain.schemaLocation`.

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
explain:
  schemaLocation:
    - https://raw.githubusercontent.com/controlplaneio-fluxcd/schema-catalog/main/catalog
```

## Resource References

`explain` resolves resource references in the same style as `kubectl explain`:
kind names (`OCIRepository.spec.verify`), plural resource names
(`ocirepositories.spec.verify`), full resource names
(`ocirepositories.source.toolkit.fluxcd.io.spec.verify`), and short names
(`po.spec`, `hr.spec`, `ag.spec`). Built-in Kubernetes short names are
recognized by the command. CRD kinds, plural names, singular names, full
resource names, and short names are resolved from sharded metadata under
`.explain/refs/` when the catalog provides it. Shell completion uses
`.explain/completion/` shards and suggests the canonical resource name
(`plural.group`, or `plural` for core resources) for all indexed resources
matching the typed prefix.

When `--api-version` is set, dotted group suffixes are treated like kubectl:
the first path segment is the resource name and the remaining segments are
field names. Without `--api-version`, `explain` first tries to interpret dotted
segments after the resource as an API group, then falls back to field lookup.

## Catalogs

For best parity with kubectl, use a description-preserving catalog generated
with:

```shell
flux schema extract k8s --with-explain-metadata --with-field-index -d ./catalog
flux schema extract crd --with-explain-metadata --with-field-index -d ./catalog
```

`--with-explain-metadata` is opt-in. It keeps validation-only catalogs small
by default, and when enabled it adds `x-flux-schema-*` annotations, small alias
redirect files, `.explain/refs/` exact lookup files, and `.explain/completion/`
prefix shards used to resolve kubectl-style resource references and provide
type-reference shell completion. Those annotations let `explain` recover exact
kind names, discovery aliases, and named field types such as `ObjectMeta`,
`PodSpec`, and `HelmReleaseSpec`.

Catalogs generated with `--strip-description` still resolve fields, but
descriptions are empty and nested object type names may fall back to `Object`
when the schema lacks explain metadata.
