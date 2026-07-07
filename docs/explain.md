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
flux schema explain hr.spec -s ecosystem

# Explain a nested field
flux schema explain pods.spec.containers --api-version=v1 --schema-location ./catalog

# Print nested fields recursively
flux schema explain pods --api-version=v1 --recursive --schema-location ./catalog
```

## Flags

| Flag                         | Description                                                                                              |
|------------------------------|----------------------------------------------------------------------------------------------------------|
| `-s, --schema-location`      | URL or file path for schemas (repeatable); `default` points at the built-in validation catalog, `ecosystem` at the CNCF ecosystem catalog. |
| `-f, --config`               | YAML config file with `explain` defaults; defaults to `$FLUX_SCHEMA_CONFIG`, else `<executable>.config`. |
| `--api-version`              | Get different explanations for a particular API version (`group/version`).                               |
| `--recursive`                | Print fields of fields.                                                                                  |
| `-o, --output`               | Output format, one of `plaintext` or `plaintext-openapiv2` (default: `plaintext`).                       |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification when fetching schemas over HTTPS.                                   |

When `--schema-location` is not passed, `explain` reads config from
`--config`, then `$FLUX_SCHEMA_CONFIG`, then a file next to the running binary
named `<binary>.config`. That config must set `explain.schemaLocation`.

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
explain:
  schemaLocation:
    - https://schemas.fluxoperator.dev/catalog
```

## Resource References

`explain` resolves resource references in the same style as `kubectl explain`:
kind names (`OCIRepository.spec.verify`), plural resource names
(`ocirepositories.spec.verify`), full resource names
(`ocirepositories.source.toolkit.fluxcd.io.spec.verify`), and short names
(`po.spec`, `hr.spec`, `ag.spec`). Built-in Kubernetes short names are
recognized by the command. CRD kinds, plural names, singular names, full
resource names, and short names are resolved from sharded metadata under
`.explain/refs/` when custom catalogs provide it. For the `ecosystem` catalog,
`explain` uses the hosted `https://schemas.fluxoperator.dev/index.json` to
resolve resource references and provide shell completion without fetching
thousands of per-resource metadata files. Resource completion suggests the
canonical resource name (`plural.group`, or `plural` for core resources) for all
indexed resources matching the typed prefix. Field completion resolves the typed
resource reference, fetches that schema, and suggests matching child field paths
while preserving the resource reference the user typed. Loaded schemas and
not-found lookups are cached in memory for the life of the process; no disk
cache is written.

When `--api-version` is set, dotted group suffixes are treated like kubectl:
the first path segment is the resource name and the remaining segments are
field names. Without `--api-version`, `explain` first tries to interpret dotted
segments after the resource as an API group, then falls back to field lookup.

## Catalogs

`explain` has two catalog modes.

### Ecosystem index mode

The hosted `ecosystem` catalog is special. `flux schema explain -s ecosystem`
uses `https://schemas.fluxoperator.dev/index.json` for resource lookup and
resource-name completion. Field completion and command output fetch only the
schema JSON needed for the resolved resource. Because the index already provides
plural names, short names, full resource names, and resource completion
candidates, the catalog does not need alias redirect files or a `.explain/`
tree.

Schemas used with this mode should be generated with JSON-only explain type
metadata:

```shell
flux schema extract k8s --with-explain-type-metadata -d ./catalog
```

`--with-explain-type-metadata` keeps only schema-local JSON hints used after a
schema has been loaded, such as named field types (`Container`, `Quantity`,
`IntOrString`) and referenced type descriptions. It does not write alias JSON
files, `.explain/refs/`, or `.explain/completion/`.

### Standalone catalog mode

Use standalone mode for custom catalogs that must contain everything
`flux schema explain` needs without the ecosystem index.

```shell
flux schema extract k8s --with-explain-metadata -d ./catalog
flux schema extract crd --with-explain-metadata -d ./catalog
```

`--with-explain-metadata` is the full explain mode. It is a superset of
`--with-explain-type-metadata`: it keeps the schema-local JSON type hints and
also writes alias redirects, `.explain/refs/` lookup files, and
`.explain/completion/` shards used for kubectl-style resource references and
type-reference shell completion.

`--with-field-index` is independent from both modes. It writes `.fields.txt`
sidecars for search and agent workflows. `explain` does not use field indexes
for resource lookup, completion, field rendering, type names, or descriptions.
It can read them only as a fallback for root `apiVersion` and `kind` when JSON
explain metadata is missing.

Catalogs generated with `--strip-description` still resolve fields, but they
cannot reproduce kubectl's descriptive output byte-for-byte because regular
descriptions and explain type descriptions are removed.
