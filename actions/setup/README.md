# Setup Flux Schema CLI GitHub Action

This GitHub Action can be used to install the Flux Schema CLI on GitHub runners for usage in workflows.
All GitHub runners are supported, including Ubuntu, Windows, and macOS.

## Usage

Example workflow for validating a Helm chart in pull requests:

```yaml
name: flux-schema

on:
  pull_request:
    branches: [main]

jobs:
  validate-chart:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v6
      - name: Setup Flux Schema CLI
        uses: fluxcd/flux-schema/actions/setup@main
      - name: Validate chart
        run: |
          helm template ./charts/app | \
          flux-schema validate --skip-missing-schemas -o json
```

## Action Inputs

| Name                 | Description                              | Default                   |
|----------------------|------------------------------------------|---------------------------|
| `version`            | Flux Schema version                      | The latest stable release |
| `bindir`             | Alternative location for the CLI         | `$RUNNER_TOOL_CACHE`      |
| `verify-attestation` | Verify the release attestation with `gh` | `true`                    |

## Action Outputs

| Name               | Description                                                |
|--------------------|------------------------------------------------------------|
| `version`          | The Flux Schema CLI version that was effectively installed |
