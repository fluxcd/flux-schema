# Validate GitOps Repository GitHub Action

This GitHub Action validates Kubernetes YAML manifests and kustomize overlays
against the Flux Schema catalog, including CEL rule evaluation. It is intended
to run in CI before changes are merged to a branch synced by Flux.

The action wraps [`validate.sh`](validate.sh), which auto-detects kustomize
overlays and skips YAML files included in Helm chart and Terraform modules.
Helm charts can be opted into validation with the `helm-charts` input, which
renders them with `helm template` using their default values.

## Prerequisites

The action expects `kubectl` (for building overlays with `kubectl kustomize`)
and `flux-schema` to be on `PATH`, plus `helm` when `helm-charts` is enabled.
`kubectl` and `helm` are pre-installed on GitHub-hosted runners; compose with
[`fluxcd/flux-schema/actions/setup`](../setup) to install the CLI.

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
apiVersion: schema.plugin.fluxcd.io/v1beta1
kind: Config
validate:
  schemaLocation:
    - default
    - ecosystem
  skipJSONPath:
    - Secret:/sops
  skipMissingSchemas: true
  verbose: true
  output: text
```

See the [validate config file reference](../../docs/manifests-validation.md#config-file)
for the full list of supported keys.

If the file lives at a non-default path, point the action at it with `config`:

```yaml
- uses: fluxcd/flux-schema/actions/validate@main
  with:
    config: .github/.fluxschema.yml
```

### Validating Helm charts

Helm chart directories (containing a `Chart.yaml`) are excluded from
validation by default, since chart templates are not valid Kubernetes YAML
until rendered. Set `helm-charts: "true"` to render each chart with
`helm template` using its default values and validate the output:

```yaml
- uses: fluxcd/flux-schema/actions/validate@main
  with:
    helm-charts: "true"
```

Charts vendored inside another chart's `charts/` directory are rendered as
part of their parent and are not templated standalone. Charts with remote
dependencies must have them vendored (`helm dependency build`) before
validation, otherwise the render fails and is reported as an error.

### Writing a manifest bundle

With `output-bundle`, the action merges every standalone manifest and the
rendered output of every kustomize overlay into a single YAML file. Missing
parent directories are created. Use a dot-prefixed name such as `.bundle.yaml`:
dotfiles are excluded from validation and from `flux-schema discover` by
default, so the bundle never shows up as a manifest in later runs. Each unit
is preceded by a provenance comment naming its origin, with paths relative to
the validated root:

```yaml
---
# === file: infrastructure/sources.yaml ===
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
...
---
# === kustomize-overlay: clusters/production ===
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
...
```

With `helm-charts` enabled, rendered charts are bundled as well under
`# === helm-chart: <dir> ===` headers. Overlays and charts whose build fails
are recorded with a `(build failed)` marker. A build or validation failure
does not stop the run: all remaining files, overlays and charts are still
validated and bundled, and the action fails at the end with the total error
count.

The bundle contains the post-build state of the repository (after kustomize
patches and generators are applied), making it a single greppable audit
surface for tools and AI agents. It can be uploaded as a workflow artifact:

```yaml
- name: Validate manifests
  uses: fluxcd/flux-schema/actions/validate@main
  with:
    output-bundle: .bundle.yaml
- name: Upload bundle
  if: always()
  uses: actions/upload-artifact@v4
  with:
    name: manifest-bundle
    path: .bundle.yaml
    include-hidden-files: true
```

## Action Inputs

| Name            | Description                                                                                                                   | Default           |
|-----------------|-------------------------------------------------------------------------------------------------------------------------------|-------------------|
| `path`          | Root directory to validate (relative to the repository root).                                                                 | `.`               |
| `exclude`       | Newline-separated list of directories to exclude from validation and the bundle.                                              | `""`              |
| `config`        | Path to Flux Schema CLI config file. When the file does not exist, sensible defaults targeting the built-in catalog are used. | `.fluxschema.yml` |
| `helm-charts`   | Render Helm charts with `helm template` using their default values and validate the output. Requires `helm` on `PATH`.        | `"false"`         |
| `output-bundle` | Path to a file where all manifests and rendered overlays are merged as a single YAML stream with provenance comments.         | `""`              |
