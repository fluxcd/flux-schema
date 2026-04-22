# JSON Schema Catalog

The `catalog/latest/` directory contains JSON schemas extracted from the latest
stable releases of Kubernetes and Flux. The directory is kept up to date by a
GitHub Actions workflow.

Schemas are laid out as `<group>/<kind>_<version>.json`.

## Kubernetes APIs

Extracted from the OpenAPI v2 swagger of the latest stable version of Kubernetes.

- `core` — the `v1` API (`Pod`, `Service`, `ConfigMap`, `Secret`, `Namespace`, ...)
- `apps`, `autoscaling`, `batch`, `extensions`, `policy`
- `admission.k8s.io`, `admissionregistration.k8s.io`
- `apiextensions.k8s.io`, `apiregistration.k8s.io`
- `authentication.k8s.io`, `authorization.k8s.io`
- `certificates.k8s.io`, `coordination.k8s.io`
- `discovery.k8s.io`, `events.k8s.io`
- `flowcontrol.apiserver.k8s.io`, `imagepolicy.k8s.io`
- `networking.k8s.io`, `node.k8s.io`
- `rbac.authorization.k8s.io`, `resource.k8s.io`
- `scheduling.k8s.io`, `storage.k8s.io`, `storagemigration.k8s.io`

## Flux APIs

Extracted from the CRDs shipped by the latest stable Flux distribution and the Flux Operator.

- `helm.toolkit.fluxcd.io` — `HelmRelease`
- `image.toolkit.fluxcd.io` — `ImagePolicy`, `ImageRepository`, `ImageUpdateAutomation`
- `kustomize.toolkit.fluxcd.io` — `Kustomization`
- `notification.toolkit.fluxcd.io` — `Alert`, `Provider`, `Receiver`
- `source.toolkit.fluxcd.io` — `Bucket`, `ExternalArtifact`, `GitRepository`, `HelmChart`, `HelmRepository`, `OCIRepository`
- `source.extensions.fluxcd.io` — `ArtifactGenerator`
- `fluxcd.controlplane.io` — `FluxInstance`, `FluxReport`, `ResourceSet`, `ResourceSetInputProvider`
