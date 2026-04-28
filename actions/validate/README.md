# Validate GitOps Repository GitHub Action

This GitHub Action validates Kubernetes YAML manifests and kustomize overlays
against the Flux Schema catalog, including CEL rule evaluation. It is intended
to run in CI before changes are merged to a branch synced by Flux.

The action wraps [`validate.sh`](validate.sh), which auto-detects kustomize
overlays and skips YAML files included in Helm chart and Terraform modules.

## Prerequisites

The action expects `kubectl` (for building overlays with `kubectl kustomize`)
and `flux-schema` to be on `PATH`. `kubectl` is pre-installed on GitHub-hosted
runners; compose with [`fluxcd/flux-schema/actions/setup`](../setup)
to install the CLI.

## Usage

Example workflow for validating pull requests:

```yaml
name: validate

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
        with:
          path: "."
          exclude: |
            config/testdata
```

By default, the action looks for a `.fluxschema.yml` file at the repository
root. When present, all validation options (schema locations, skipped kinds,
SOPS field stripping, etc.) are read from it. When absent, the action falls
back to a built-in set of defaults that targets the Flux Schema catalog.

### Using a config file

Commit a `.fluxschema.yml` to your repository root to control the validation:

```yaml
# .fluxschema.yml
version: "1"
validate:
  schema-location:
    - default
    - https://raw.githubusercontent.com/datreeio/CRDs-catalog/main
  skip-json-path:
    - Secret:/sops
  skip-missing-schemas: true
  verbose: true
  output: text
```

See the [validate config file reference](../../docs/guides/manifests-validation.md#config-file)
for the full list of supported keys.

If the file lives at a non-default path, point the action at it with `config`:

```yaml
- uses: fluxcd/flux-schema/actions/validate@main
  with:
    config: .github/.fluxschema.yml
```

## Action Inputs

| Name      | Description                                                                                                                   | Default           |
|-----------|-------------------------------------------------------------------------------------------------------------------------------|-------------------|
| `path`    | Root directory to validate (relative to the repository root).                                                                 | `.`               |
| `exclude` | Newline-separated list of directories to exclude from validation.                                                             | `""`              |
| `config`  | Path to Flux Schema CLI config file. When the file does not exist, sensible defaults targeting the built-in catalog are used. | `.fluxschema.yml` |
