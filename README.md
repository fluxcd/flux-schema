# flux-schema

[![release](https://img.shields.io/github/release/fluxcd/flux-schema/all.svg)](https://github.com/fluxcd/flux-schema/releases)
[![test](https://github.com/fluxcd/flux-schema/actions/workflows/test.yaml/badge.svg)](https://github.com/fluxcd/flux-schema/actions/workflows/test.yaml)
[![cve-scan](https://github.com/fluxcd/flux-schema/workflows/cve-scan/badge.svg)](https://github.com/fluxcd/flux-schema/actions/workflows/cve-scan.yml)
[![license](https://img.shields.io/github/license/fluxcd/flux-schema.svg)](https://github.com/fluxcd/flux-schema/blob/main/LICENSE)
[![slsa](https://slsa.dev/images/gh-badge-level2.svg)](https://github.com/fluxcd/flux-schema/attestations)

**Flux Schema** is a CLI for validating Kubernetes YAML manifests against JSON
Schema and CEL rules using the same evaluation semantics as the Kubernetes
API server. It ships as a single Go binary with a built-in catalog covering
Kubernetes, OpenShift, Gateway API and the Flux ecosystem CRDs.

This project is inspired by `kubeconform`, adding CEL rule evaluation,
built-in schema extraction for CRDs & OpenAPI swagger, and a curated catalog
refreshed automatically from upstream stable releases.

> [!NOTE]
> This repository is in early development and the plugin system is not yet
> available in a stable release of Flux. Instructions for installing and
> using `flux-schema` as a Flux CLI plugin will be added here once
> [RFC-0013](https://github.com/fluxcd/flux2/blob/main/rfcs/0013-cli-plugin-system/README.md)
> ships in Flux 2.9 or later.

## Features

- **Strict schema validation** — every field of every Kubernetes built-in
  kind and custom resource is checked. Unknown fields, wrong types, and
  missing required properties are all reported as schema violations.
- **CEL evaluation** — `x-kubernetes-validations` rules evaluated with the
  same engine as Kubernetes API server.
- **Strict YAML decoding** — duplicate keys are rejected matching Flux
  behavior. Metadata name, namespace, labels, and annotations are
  checked against API server rules (DNS-1123, qualified names).
- **Built-in catalog** — JSON Schemas with CEL rules for Kubernetes, OpenShift,
  Gateway API, Flux, Flagger and Flux Operator CRDs, refreshed automatically against upstream.
- **Custom catalogs** — extract JSON Schemas from Kubernetes CRDs and OpenAPI swagger files,
  then layer your catalog on top of the default schemas.
- **SOPS-aware** — strip SOPS metadata fields so the rest of the document can be validated without decryption.
- **Structured reports** — versioned JSON or YAML validation reports for CI tooling and downstream automation.
- **Declarative validation** — define the validation config in a `.fluxschema.yml` file for reproducible runs across local and CI environments.
- **GitHub Actions** — composite actions for installation and manifests validation on GitHub runners.

## Install

Download the binary for your platform from the [releases page](https://github.com/fluxcd/flux-schema/releases),
or build from source:

```shell
go install github.com/fluxcd/flux-schema/cmd/flux-schema@latest
```

For GitHub Actions runners, use the [`actions/setup`](actions/setup) action.

## Quickstart

Validate a directory tree against the built-in catalog and 3rd-party schemas:

```shell
flux-schema validate ./manifests \
--schema-location default \
--schema-location https://raw.githubusercontent.com/datreeio/CRDs-catalog/main
```

Build a kustomize overlay and validate the generated manifests:

```shell
kustomize build ./clusters/production | flux-schema validate --verbose
```

Render a Helm chart and validate the generated manifests:

```shell
helm template ./charts/app | flux-schema validate -v --skip-missing-schemas
```

Build a [ResourceSet](https://fluxoperator.dev/docs/resourcesets/introduction/) and validate the generated manifests:

```shell
flux-operator build rset -f tenants.yaml | flux-schema validate
```

Emit a structured report for CI tooling:

```shell
flux-schema validate ./manifests -o json
```

Extract JSON Schemas from your CRDs and layer them on top of the built-in catalog:

```shell
kubectl get crds -o yaml | flux-schema extract crd -d ./my-catalog

flux-schema validate ./manifests \
  --schema-location ./my-catalog \
  --schema-location default
```

## Running in CI

Running flux-schema in CI shifts validation left, so schema violations are
caught in pull requests rather than on the cluster after Flux reconciles them.
The CLI ships as GitHub Actions for repositories hosted on GitHub,
and as a multi-arch (AMD64 and ARM64) container image for other CI
systems and air-gapped environments.

### GitHub Actions

Two composite actions cover GitOps validation pipelines:

- **[`fluxcd/flux-schema/actions/setup`](actions/setup)** — install the CLI on GitHub runners.
- **[`fluxcd/flux-schema/actions/validate`](actions/validate)** —
  auto-detect kustomize overlays, render them with `kubectl kustomize`, and
  validate every YAML document against the catalog (including CEL rules).
  Configurable via `.fluxschema.yml`.

Minimal pull-request workflow:

```yaml
name: flux-schema

on:
  pull_request:
    branches: [main]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v6
      - name: Setup Flux Schema CLI
        uses: fluxcd/flux-schema/actions/setup@main
      - name: Validate manifests
        uses: fluxcd/flux-schema/actions/validate@main
```

### Docker

The `ghcr.io/fluxcd/flux-schema` image bakes the built-in catalog into
`/catalog/latest/`, so it can validate manifests in air-gapped environments:

```shell
docker run --rm \
  -v "$PWD/manifests:/manifests:ro" \
  ghcr.io/fluxcd/flux-schema:latest validate /manifests \
  --schema-location /catalog/latest
```

See the [Docker section](docs/guides/manifests-validation.md#docker) of the
validation guide for details on running the CLI in CI using the container image.

## Commands

| Command                                   | Description                                                 |
|-------------------------------------------|-------------------------------------------------------------|
| `flux-schema validate [paths...]`         | Validate Kubernetes YAML against JSON Schema and CEL rules. |
| `flux-schema extract crd [files...]`      | Extract JSON Schemas from CRD YAMLs.                        |
| `flux-schema extract k8s [swagger]`       | Extract JSON Schemas from Kubernetes OpenAPI v2 swagger.    |
| `flux-schema extract openshift [swagger]` | Extract JSON Schemas from OpenShift OpenAPI v2 swagger.     |

Run `flux-schema <command> --help` for the full flag list.

## Documentation

- [Manifest validation guide](docs/guides/manifests-validation.md) — flag
  reference, schema resolution, CEL rules, skipping documents and fields,
  and config files.
- [Custom catalog guide](docs/guides/custom-schema-catalog.md) — populate,
  layout, host, and refresh your own catalog with the `extract` commands.
- [Validation report reference](docs/report/README.md) — envelope shape and
  JSON Schema for `-o json` / `-o yaml` output.
- [Built-in catalog](catalog/README.md) — Kubernetes, OpenShift, Gateway
  API, and Flux ecosystem CRDs covered by the default `default` schema location.

## License

The Flux Schema project is [Apache 2.0 licensed](LICENSE) and accepts contributions
via GitHub pull requests.
