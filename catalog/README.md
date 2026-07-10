# Flux Schema Catalog

This is the catalog of JSON Schemas used by the Flux Schema validation tool
and the GitHub Action [fluxcd/flux-schema/actions/validate](../actions/validate).

For CRDs not covered by the built-in catalog, the ecosystem catalog is available
via `--schema-location ecosystem` and browsable at https://schemas.fluxoperator.dev.

<!-- versions:start -->
| Source | Version |
| --- | --- |
| [kubernetes/kubernetes](https://github.com/kubernetes/kubernetes) | v1.36.2 |
| [kubernetes-sigs/gateway-api](https://github.com/kubernetes-sigs/gateway-api) | v1.6.0 |
| [openshift/api](https://github.com/openshift/api) | v4.22 |
| [fluxcd/flux2](https://github.com/fluxcd/flux2) | v2.9.1 |
| [fluxcd/flagger](https://github.com/fluxcd/flagger) | v1.43.0 |
| [controlplaneio-fluxcd/flux-operator](https://github.com/controlplaneio-fluxcd/flux-operator) | v0.54.1 |
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

## Gateway API

Extracted from the CRDs shipped by the latest stable release of the Kubernetes Gateway API.

- `gateway.networking.k8s.io` — `Gateway`, `GatewayClass`, `GRPCRoute`, `HTTPRoute`, etc.

If you need schemas for the Gateway API [experimental](https://gateway-api.sigs.k8s.io/concepts/versioning/)
channel, you can generate them with:

```shell
kubectl kustomize https://github.com/kubernetes-sigs/gateway-api/config/crd/experimental?ref=main | \
  flux schema extract crd -d ./gwapi-experimental
```

And use them with `--schema-location`, before the `default` catalog, when validating:

```shell
flux schema validate ./manifests \
  --schema-location ./gwapi-experimental \
  --schema-location default
```

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

## OpenShift APIs

Extracted from the OpenAPI v2 swagger of the latest stable release of OpenShift.

- `apps.openshift.io`, `route.openshift.io`, `build.openshift.io`, `image.openshift.io`,
  `template.openshift.io`, `project.openshift.io`, `quota.openshift.io`,
  `user.openshift.io`, `oauth.openshift.io`, `console.openshift.io`,
  `monitoring.openshift.io`, `helm.openshift.io`, `samples.operator.openshift.io`
- `config.openshift.io`, `operator.openshift.io`, `machine.openshift.io`,
  `machineconfiguration.openshift.io`, `network.openshift.io`,
  `authorization.openshift.io`, `security.openshift.io`, `apiserver.openshift.io`
- `cloud.network.openshift.io`, `network.operator.openshift.io`,
  `ingress.operator.openshift.io`, `controlplane.operator.openshift.io`,
  `kubecontrolplane.config.openshift.io`,
  `openshiftcontrolplane.config.openshift.io`, `osin.config.openshift.io`,
  `servicecertsigner.config.openshift.io`, `security.internal.openshift.io`
