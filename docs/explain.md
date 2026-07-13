---
weight: 25
---

# Explain Kubernetes Resources

`flux schema explain` prints kubectl-style documentation for Kubernetes
resources and fields. It reads JSON Schema catalogs and does not contact a
Kubernetes API server.

## Quick Start

For most users, the hosted ecosystem catalog is the right choice:

```shell
flux schema explain helmreleases -s ecosystem
```

Add a dot-separated field path to explain a specific field:

```shell
flux schema explain helmreleases.spec.chart -s ecosystem
```

Other common examples:

```shell
# Include nested fields
flux schema explain pods.spec --recursive -s ecosystem

# Select an API version
flux schema explain deployments --api-version=apps/v1 -s ecosystem
```

`explain` does not support the built-in validation catalog selected by
`-s default`. Use `-s ecosystem` or a custom catalog generated with explain
metadata.

## Choose a Catalog

`explain` requires either the ecosystem catalog or a custom catalog. If you
omit `-s`, [configure a catalog](#configure-a-catalog) instead.

### Ecosystem catalog

Use `-s ecosystem` for the hosted catalog at
[schemas.fluxoperator.dev](https://schemas.fluxoperator.dev). It includes
Kubernetes APIs and CNCF ecosystem resources with field descriptions.

```shell
flux schema explain hr.spec -s ecosystem
```

### Custom catalog

Use a custom catalog for schemas you build yourself, whether they are stored
locally or hosted on your own server. Generate it with full explain metadata:

```shell
flux schema extract k8s --with-explain-metadata -d ./my-catalog
flux schema extract crd crds.yaml --with-explain-metadata -d ./my-catalog
```

Then pass its directory or URL:

```shell
flux schema explain pods -s ./my-catalog
```

See the [custom catalog guide](custom-schema-catalog.md) for extraction,
layout, and hosting details.

## Resource and Field References

`TYPE` can name a resource or a field within that resource.

| Reference | Example |
|-----------|---------|
| Kind | `OCIRepository.spec.verify` |
| Plural resource name | `ocirepositories.spec.verify` |
| Fully qualified resource name | `ocirepositories.source.toolkit.fluxcd.io.spec.verify` |
| Short name | `po.spec`, `hr.spec`, `ag.spec` |

Built-in Kubernetes short names are recognized automatically. The ecosystem
index and custom catalog metadata provide names for CRDs.

Pass `--api-version=group/version` to select an API version explicitly. With
this flag, every segment after the resource name is treated as a field. Without
it, `explain` first checks whether dotted segments identify an API group, then
falls back to treating them as fields.

## Flags

| Flag | Description |
|------|-------------|
| `-s, --schema-location` | `ecosystem`, or a custom catalog URL, path, or template. Repeat to try multiple catalogs in order. |
| `-f, --config` | YAML configuration file. |
| `--api-version` | API version to explain, in `group/version` form. |
| `--recursive` | Include nested fields, one level deep. |
| `-o, --output` | `plaintext` or `plaintext-openapiv2`; defaults to `plaintext`. |
| `--insecure-skip-tls-verify` | Disable TLS certificate verification for HTTPS catalogs. |

## Configure a Catalog

When `-s` is omitted, `explain` reads `explain.schemaLocation` from the first
configured source:

1. The file passed with `--config`.
2. The file named by `FLUX_SCHEMA_CONFIG`.
3. A `<binary>.config` file next to the running executable.

The configuration must select `ecosystem` or a custom catalog:

```yaml
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
explain:
  schemaLocation:
    - ecosystem
```

With that configuration, the catalog flag can be omitted:

```shell
flux schema explain pods.spec
```

## Detailed Behavior

The remaining sections describe catalog lookup and metadata. They are mainly
useful when building catalogs or integrating shell completion.

### Ecosystem catalog lookup

The ecosystem catalog uses
`https://schemas.fluxoperator.dev/index.json` for resource lookup and
resource-name completion. This has two practical effects:

- Only the schema needed for the requested resource is fetched.
- The hosted catalog does not need alias redirect files or a `.explain/` tree.

Its schemas contain JSON-only type metadata generated with
`--with-explain-type-metadata`. This preserves named field types such as
`Container`, `Quantity`, and `IntOrString`, plus referenced type descriptions.

### Custom catalog metadata

`--with-explain-metadata` writes everything a custom catalog needs:

- Schema-local type hints and referenced type descriptions.
- Alias redirect JSON files.
- `.explain/refs/` files for resource lookup.
- `.explain/completion/` shards for resource-name completion.

This flag is a superset of `--with-explain-type-metadata`. Use the type-only
flag when a separate index supplies resource lookup and completion, as the
ecosystem catalog does.

### Shell completion and caching

Resource completion returns the canonical name: `plural.group` for grouped
resources and `plural` for core resources. Field completion fetches the
resolved schema and suggests child field paths while preserving the resource
reference that was typed.

Loaded schemas and not-found lookups are cached in memory for the life of the
process. No disk cache is written.

### Field indexes and descriptions

`--with-field-index` is independent of explain metadata. Its `.fields.txt`
files are intended for search, agents, and other catalog consumers. `explain`
only reads them as a fallback for root `apiVersion` and `kind` when JSON explain
metadata is missing.

Catalogs generated with `--strip-description` can still resolve fields, but
their descriptive output is limited because regular and explain type
descriptions have been removed.
