---
weight: 22
---

# Kubernetes Manifests Build with Flux Schema CLI

The `flux schema build` command renders Kubernetes YAML manifests from one
or more files or directories and writes them to stdout as a single YAML stream,
the same way `flux schema validate` sees them.

Examples:

```shell
# Render plain YAML files and build kustomize directories in a tree
flux schema build ./clusters/production > .bundle.yaml

# Apply Flux post-build variable substitution
flux schema build ./clusters/production --envsubst-file .env
```

Paths are handled like in `validate`: directories are walked, a directory
with a kustomization file is [built with kustomize](manifests-validation.md#kustomize-build),
and `-` reads from stdin.

## Output

Each document is preceded by a `# Source:` comment with the same value
`validate` reports as `source`. Resources built from a file also get an
`# Origin:` comment:

```yaml
---
# Source: clusters/production
# Origin: apps/base/podinfo/release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
...
---
# Source: infrastructure/sources.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
...
```

Documents are written as read unless variable substitution changes them.
Comment-only documents are dropped.

## Variable substitution

With `--envsubst-file`, documents are substituted with the same rules as
[validation](manifests-validation.md#variable-substitution). Substituted documents
are written after re-serialization, as Flux applies them.

## Flags

| Flag                | Description                                                                                                              |
|---------------------|--------------------------------------------------------------------------------------------------------------------------|
| `--envsubst-file`   | Path to a dotenv file supplying Flux post-build substitution variables.                                                  |
| `--envsubst-strict` | Fail on undefined substitution variables that have no default; requires `--envsubst-file`.                               |
| `--skip-file`       | Basename glob for files and dirs; skipping a kustomization file restores per-file rendering (repeatable, default: `.*`). |

## Errors

Read, kustomize build and substitution errors are printed to stderr. The remaining
documents are still rendered and the command exits with a non-zero code.
