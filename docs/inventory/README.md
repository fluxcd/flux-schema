# Flux Schema Inventory

The `flux-schema discover` command can emit a structured inventory of a
GitOps repository by setting `--output` to `json` or `yaml`. The envelope
shape is versioned and documented by the JSON Schema in
[`inventory-v1beta1.json`](inventory-v1beta1.json).

## Usage

```shell
flux-schema discover ./my-gitops-repo -o json
```

See the [repository discovery guide](../guides/repo-discovery.md) for the
classification rules behind the inventory data.

## Envelope

Every inventory is wrapped in a top-level envelope:

| Key                     | Description                                                         |
|-------------------------|---------------------------------------------------------------------|
| `apiVersion`            | Inventory API version. Currently `schema.plugin.fluxcd.io/v1beta1`. |
| `kind`                  | Inventory API kind. Currently `Inventory`.                          |
| `$schema`               | URL of the JSON Schema describing the envelope.                     |
| `inventory.reporter`    | Identity of the producer, e.g. `flux-schema/0.3.0`.                 |
| `inventory.timestamp`   | RFC 3339 UTC timestamp of the scan.                                 |
| `inventory.summary`     | Aggregate counts: `files`, `resources`, `lines-of-yaml`.            |
| `inventory.directories` | Map of directory path to classification.                            |
| `inventory.resources`   | Map of `apiVersion/Kind` to resource count, for all resources.      |
| `inventory.flux`        | Map of Flux resource kind to defining files and identities.         |

All maps serialize with sorted keys, so the output is deterministic and
diff-friendly.

## Directories

`directories` maps every directory containing scanned manifests, plus
Helm chart and Terraform directories, to one of four classifications:
`kubernetes-manifests`, `kustomize-overlay`, `helm-chart`, or `terraform-module`. Paths are
relative to the scanned root and `/`-separated; the root itself is `.`.

```json
"directories": {
  "apps/base": "kustomize-overlay",
  "charts/podinfo": "helm-chart"
}
```

## Resources

`resources` is the complete census: every resource in the repository,
counted per `apiVersion/Kind`. The exact
API version of every kind is visible without opening any file.

```json
"resources": {
  "apps/v1/Deployment": 12,
  "cert-manager.io/v1/ClusterIssuer": 1,
  "helm.toolkit.fluxcd.io/v2/HelmRelease": 3
}
```

## Flux

`flux` locates every Flux custom resource. Keys are bare kinds — their
API versions are already stated in `resources`. Each kind maps the
defining file path (relative to the scanned root) to the resource
identities declared in that file:

```json
"flux": {
  "HelmRelease": {
    "apps/base/podinfo.yaml": ["podinfo/podinfo"]
  },
  "Kustomization": {
    "clusters/production/apps.yaml": ["flux-system/apps"],
    "clusters/staging/apps.yaml": ["flux-system/apps"]
  }
}
```

Identities are `namespace/name`; the `namespace/` segment is omitted for
resources without a namespace, and the name is `-` when the document has
no `metadata.name`. Multi-document files list their identities in
document order.

Plain Kubernetes resources are intentionally not enumerated per item: a
large repository can hold thousands of them, and the census combined
with the directory map locates them precisely enough.

When the same kind exists in more than one API version — typically
mid-migration — its files merge under the kind key, while
`resources` still reports each version separately.

## Example

```json
{
  "apiVersion": "schema.plugin.fluxcd.io/v1beta1",
  "kind": "Inventory",
  "$schema": "https://raw.githubusercontent.com/fluxcd/flux-schema/main/docs/inventory/inventory-v1beta1.json",
  "inventory": {
    "reporter": "flux-schema/0.3.0",
    "timestamp": "2026-06-11T10:30:00Z",
    "summary": {
      "files": 8,
      "resources": 8,
      "lines-of-yaml": 99
    },
    "directories": {
      "apps/base": "kustomize-overlay",
      "apps/overlays/prod": "kustomize-overlay",
      "charts/podinfo": "helm-chart",
      "clusters/prod": "kubernetes-manifests",
      "infra/tf": "terraform-module"
    },
    "resources": {
      "apps/v1/Deployment": 1,
      "fluxcd.controlplane.io/v1/FluxInstance": 1,
      "helm.toolkit.fluxcd.io/v2/HelmRelease": 1,
      "kustomize.toolkit.fluxcd.io/v1/Kustomization": 1,
      "source.toolkit.fluxcd.io/v1/GitRepository": 1,
      "source.toolkit.fluxcd.io/v1/OCIRepository": 1,
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
      },
      "OCIRepository": {
        "apps/base/podinfo.yaml": ["podinfo/podinfo"]
      }
    }
  }
}
```
