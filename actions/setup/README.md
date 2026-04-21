# Setup Flux Schema CLI GitHub Action

This GitHub Action can be used to install the Flux Schema CLI on GitHub runners for usage in workflows.
All GitHub runners are supported, including Ubuntu, Windows, and macOS.

## Usage

Example workflow for printing the latest version:

```yaml
name: Check the latest version

on:
  workflow_dispatch:

jobs:
  check-latest-flux-schema-version:
    runs-on: ubuntu-latest
    steps:
      - name: Setup Flux Schema CLI
        uses: fluxcd/flux-schema/actions/setup@main
        with:
          version: latest
      - name: Print Flux Schema Version
        run: flux-schema version
```

## Action Inputs

| Name               | Description                      | Default                   |
|--------------------|----------------------------------|---------------------------|
| `version`          | Flux Schema version              | The latest stable release |
| `bindir`           | Alternative location for the CLI | `$RUNNER_TOOL_CACHE`      |

## Action Outputs

| Name               | Description                                                |
|--------------------|------------------------------------------------------------|
| `version`          | The Flux Schema CLI version that was effectively installed |
