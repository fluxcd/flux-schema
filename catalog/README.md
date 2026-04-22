# Flux Schema Catalog

This is the catalog of JSON Schemas for Kubernetes APIs and Flux CRDs,
used by the `flux schema validation` tool.

<!-- versions:start -->
| Source | Version |
| --- | --- |
| [kubernetes/kubernetes](https://github.com/kubernetes/kubernetes) | v1.35.4 |
| [fluxcd/flux2](https://github.com/fluxcd/flux2) | v2.8.6 |
| [controlplaneio-fluxcd/flux-operator](https://github.com/controlplaneio-fluxcd/flux-operator) | v0.48.0 |
<!-- versions:end -->

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
