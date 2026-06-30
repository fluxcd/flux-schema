---
weight: 30
---

# GitOps Repository Discovery with Flux Schema CLI

The `flux schema discover` command catalogs the Kubernetes manifests in a
GitOps repository and emits a structured inventory. The inventory is
designed for AI agents and automation auditing a repository: it maps the
directory structure, lists every Flux resource with its defining file,
and counts plain Kubernetes resources.

```shell
flux schema discover ./my-gitops-repo -o json
```

The scan is purely static: no kustomize builds, no Helm rendering, no
cluster access. Resources are counted once, where they are defined on
disk. To validate the rendered output of kustomize overlays, see the
[manifest validation guide](manifests-validation.md).

## Why AI agents should use this command

An agent exploring a GitOps repository with `tree`, `ls` and `grep` sees
file names and string matches, not resources. Flux Schema discovery replaces
the read-and-grep loop that follows with one deterministic pass:

- **File names reveal nothing.** GitOps repos are full of `sync.yaml`,
  `release.yaml` and multi-document files; a file listing cannot reveal
  what resources a file holds, which API versions they use, or their
  names and namespaces. The inventory extracts all of it in one call
  instead of N file reads.
- **String matching miscounts.** `grep -rl "kind: HelmRelease"` matches
  kustomize patch files and patch-target references alongside real
  resources. The inventory excludes patch files by resolving the
  kustomization `patches` references, so every resource is counted
  exactly once across base and overlays.
- **Every kind carries its API version.** The `resources` census counts
  every resource per `apiVersion/Kind`, Flux and plain Kubernetes alike,
  so outdated API versions of any CRD are visible from the inventory
  alone.
- **The output is budgeted for context windows.** Flux resources — the
  usual audit subjects — are enumerated per file; plain Kubernetes
  resources appear only as counts (2,000 Deployments cost a few lines,
  not thousands); Helm chart and Terraform subtrees are pruned. A typical
  repository inventories in a few KB.
- **The shape is a versioned contract.** The JSON envelope is validated
  by a published [JSON Schema](inventory.md), so agents can
  parse it programmatically instead of interpreting ad-hoc shell output.

For very large repositories (hundreds of Flux resources), the inventory
grows with the Flux resource count in every output format. Scope the
scan to the subtree under audit
(`flux schema discover ./clusters/production -o json`) or exclude
directories with `--skip-file` to keep the output small.

`tree` remains useful for what `discover` deliberately ignores: READMEs,
scripts, CI configs, and other non-Kubernetes content.

## Flags

| Flag           | Default | Description                                                                                                                           |
|----------------|---------|---------------------------------------------------------------------------------------------------------------------------------------|
| `[path]`       | `.`     | Directory (or single file) to scan.                                                                                                   |
| `--skip-file`  | `.*`    | Glob pattern matched against file and directory basenames, repeatable. The default skips dotfiles and dot-directories such as `.git`. |
| `-o, --output` | `text`  | Output format: `text`, `json` or `yaml`.                                                                                              |

The command always exits zero on a successful scan, even when nothing is
found — discovery is informational. Reading from stdin is not supported:
directory classification is path-based and meaningless for a stream.

## Classification rules

Both `.yaml` and `.yml` files are scanned. Per YAML document:

- Documents without both `apiVersion` and `kind` are ignored.
- Documents in the `kustomize.config.k8s.io` group mark their directory as
  a kustomize overlay and are not counted as resources.
- Every resource is counted per `apiVersion/Kind` in the `resources`
  census.
- Documents whose API group contains `fluxcd` are Flux resources,
  additionally listed under `flux` with their defining file.

Every directory in the inventory carries one of four types:

| Type                   | Meaning                                                        |
|------------------------|----------------------------------------------------------------|
| `kubernetes-manifests` | Plain Kubernetes YAML manifests.                               |
| `kustomize-overlay`    | Contains a kustomize configuration file.                       |
| `helm-chart`           | Contains a `Chart.yaml`; the directory subtree is not scanned. |
| `terraform-module`     | Contains `.tf` files; the directory subtree is not scanned.    |

When a directory holds both a `Chart.yaml` and `.tf` files, the Helm chart classification wins.

### Kustomize patch files

Files referenced as kustomize patches (`patches`, `patchesStrategicMerge`,
`patchesJson6902`) are excluded from the inventory entirely. Patch files
look like full manifests but only describe modifications to resources
defined elsewhere, so counting them would duplicate resources across base
and overlays. Inline patches need no special handling. An overlay
directory whose only content is the kustomize configuration and patch
files still appears in `directories` as `kustomize-overlay`.

### Path handling

All `source` and `path` values in the inventory are `/`-separated and
relative to the scanned root; resources at the root itself land in the
`.` directory. Unlike Git-based tooling, `discover` does not honor
`.gitignore` — use `--skip-file` to exclude generated or vendored
directories by basename.

The scan is confined to the scanned root at the operating-system level,
so a link pointing outside the repository can never leak content into the
inventory or make the scan read arbitrary files. Symbolic links are not
followed even when they stay inside the root: the walk already visits the
real target, so following the link too would list the same resource
twice.

## Output

The default `text` output mirrors the envelope order — directory
classifications, the per-GVK resource census, the Flux resources by
defining file, and summary line:

```text
Directories:
  apps/base: kustomize-overlay
  apps/overlays/prod: kustomize-overlay
  charts/podinfo: helm-chart
  clusters/prod: kubernetes-manifests
  infra/tf: terraform-module
  legacy: kubernetes-manifests
Resources:
  apps/v1/Deployment: 1
  fluxcd.controlplane.io/v1/FluxInstance: 1
  helm.toolkit.fluxcd.io/v2/HelmRelease: 1
  kustomize.toolkit.fluxcd.io/v1/Kustomization: 1
  source.toolkit.fluxcd.io/v1/GitRepository: 1
  v1/ConfigMap: 1
  v1/Service: 1
Flux:
  FluxInstance:
    clusters/prod/instance.yaml: flux-system/flux
  GitRepository:
    clusters/prod/flux-system.yaml: flux-system/flux-system
  HelmRelease:
    apps/base/podinfo.yaml: podinfo/podinfo
  Kustomization:
    clusters/prod/flux-system.yaml: flux-system/apps
Summary: 7 resources found in 8 files with 92 lines of YAML
```

With `-o json` or `-o yaml` the command emits the versioned Inventory
envelope documented in the [inventory reference](inventory.md):
`resources` counts everything per `apiVersion/Kind`, and `flux` maps
each Flux kind to its defining files and the `namespace/name` identities
they declare (the API version is already in the census):

```json
{
  "apiVersion": "schema.plugin.fluxcd.io/v1beta1",
  "kind": "Inventory",
  "$schema": "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/inventory-v1beta1.json",
  "inventory": {
    "reporter": "flux-schema/0.3.0",
    "timestamp": "2026-06-11T10:30:00Z",
    "summary": {
      "files": 8,
      "resources": 7,
      "lines-of-yaml": 92
    },
    "directories": {
      "apps/base": "kustomize-overlay",
      "apps/overlays/prod": "kustomize-overlay",
      "charts/podinfo": "helm-chart",
      "clusters/prod": "kubernetes-manifests",
      "infra/tf": "terraform-module",
      "legacy": "kubernetes-manifests"
    },
    "resources": {
      "apps/v1/Deployment": 1,
      "fluxcd.controlplane.io/v1/FluxInstance": 1,
      "helm.toolkit.fluxcd.io/v2/HelmRelease": 1,
      "kustomize.toolkit.fluxcd.io/v1/Kustomization": 1,
      "source.toolkit.fluxcd.io/v1/GitRepository": 1,
      "v1/ConfigMap": 1,
      "v1/Service": 1
    },
    "flux": {
      "FluxInstance": {
        "clusters/prod/instance.yaml": ["flux-system/flux"]
      },
      "GitRepository": {
        "clusters/prod/flux-system.yaml": ["flux-system/flux-system"]
      },
      "HelmRelease": {
        "apps/base/podinfo.yaml": ["podinfo/podinfo"]
      },
      "Kustomization": {
        "clusters/prod/flux-system.yaml": ["flux-system/apps"]
      }
    }
  }
}
```

## Reading the inventory

The inventory answers an auditing agent's first questions without any grepping:

- **Repository pattern** — the sorted `directories` map reads as a tree.
  `GitRepository` or `OCIRepository` resources defined in the same
  directories as Flux `Kustomization` resources (typically under
  `clusters/`) mark cluster sync entrypoints; `FluxInstance` resources
  indicate a Flux Operator managed cluster.
- **Navigation** — every Flux kind in `flux` is keyed by its defining
  files, so an agent can open exactly the files relevant to an audit.
- **API currency** — the `resources` census is keyed by
  `apiVersion/Kind`, so deprecated API versions of Flux and third-party
  CRDs stand out without reading a single manifest.
- **Scope boundaries** — `helm-chart` and `terraform-module` directories exist in
  the map but contribute no resources, signaling that their contents are
  not Kubernetes manifests to lint.
