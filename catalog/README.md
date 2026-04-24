# Flux Schema Catalog

This is the catalog of JSON Schemas for Kubernetes APIs and Flux CRDs,
used by the `flux schema validation` tool.

<!-- versions:start -->
| Source | Version |
| --- | --- |
| [kubernetes/kubernetes](https://github.com/kubernetes/kubernetes) | v1.35.4 |
| [kubernetes-sigs/gateway-api](https://github.com/kubernetes-sigs/gateway-api) | v1.5.1 |
| [fluxcd/flux2](https://github.com/fluxcd/flux2) | v2.8.6 |
| [fluxcd/flagger](https://github.com/fluxcd/flagger) | v1.43.0 |
| [controlplaneio-fluxcd/flux-operator](https://github.com/controlplaneio-fluxcd/flux-operator) | v0.48.0 |
<!-- versions:end -->

## Flux APIs

Extracted from the CRDs shipped by the latest stable Flux distribution, Flagger and Flux Operator.

- `helm.toolkit.fluxcd.io` — `HelmRelease`
- `image.toolkit.fluxcd.io` — `ImagePolicy`, `ImageRepository`, `ImageUpdateAutomation`
- `kustomize.toolkit.fluxcd.io` — `Kustomization`
- `notification.toolkit.fluxcd.io` — `Alert`, `Provider`, `Receiver`
- `source.toolkit.fluxcd.io` — `Bucket`, `ExternalArtifact`, `GitRepository`, `HelmChart`, `HelmRepository`, `OCIRepository`
- `source.extensions.fluxcd.io` — `ArtifactGenerator`
- `flagger.app` — `Canary`, `MetricTemplate`, `AlertProvider`
- `fluxcd.controlplane.io` — `FluxInstance`, `FluxReport`, `ResourceSet`, `ResourceSetInputProvider`

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

## Gateway API

Extracted from the CRDs shipped by the latest stable release of the Kubernetes Gateway API.

- `gateway.networking.k8s.io` — `Gateway`, `GatewayClass`, `GRPCRoute`, `HTTPRoute`, etc.

If you need schemas for the Gateway API [experimental](https://gateway-api.sigs.k8s.io/concepts/versioning/)
channel, you can generate them with:

```shell
kubectl kustomize https://github.com/kubernetes-sigs/gateway-api/config/crd/experimental?ref=main | \
    flux-schema extract crd /dev/stdin \
    -d ./gwapi-experimental \
    -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json'
```

And use them with `--schema-location`, before the `default` catalog, when validating:

```shell
flux-schema validate ./manifests \
  --schema-location './gwapi-experimental/{{.Group}}/{{.Kind}}_{{.Version}}.json' \
  --schema-location default
```
