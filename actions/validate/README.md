# Validate GitOps Repository GitHub Action

This GitHub Action validates Kubernetes YAML manifests and kustomize overlays
against the Flux Schema catalog, including CEL rule evaluation. It is intended
to run in CI before changes are merged to a branch synced by Flux.

The action wraps [`validate.sh`](validate.sh), which
auto-detects kustomize overlays and skips Helm chart and Terraform directories.

## Prerequisites

The action expects `kubectl` (for the built-in `kubectl kustomize`)
and `flux-schema` to be on `PATH`. Compose with
[`fluxcd/flux-schema/actions/setup`](../setup) and an installer like
[`azure/setup-kubectl`](https://github.com/Azure/setup-kubectl):

```yaml
name: validate

on:
  pull_request:
    branches: [main]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: azure/setup-kubectl@v4
      - uses: fluxcd/flux-schema/actions/setup@main
      - uses: fluxcd/flux-schema/actions/validate@main
        with:
          path: ./clusters/production
          exclude: |
            charts
            terraform
          schema-location: |
            default
            https://raw.githubusercontent.com/datreeio/CRDs-catalog/main
```

## Action Inputs

| Name              | Description                                                                                                                 | Default                 |
|-------------------|-----------------------------------------------------------------------------------------------------------------------------|-------------------------|
| `path`            | Root directory to validate (relative to the repository root).                                                               | `.`                     |
| `exclude`         | Newline-separated list of directories to exclude from validation.                                                           | `""`                    |
| `schema-location` | Newline-separated list of schema sources (URL or path). The literal `default` selects the built-in catalog; tried in order. | `""` (built-in catalog) |
