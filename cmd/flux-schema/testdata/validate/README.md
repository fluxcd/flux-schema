# Generate Schemas

Install latest Flux on your cluster:

```sh
flux install --components-extra=image-reflector-controller,image-automation-controller,source-watcher
```

Build the CLI:

```sh
make build
```

Update the Flux JSON schemas:

```sh
kubectl get crds -l app.kubernetes.io/part-of=flux -o yaml | \
./bin/flux-schema extract crd /dev/stdin \
-d ./cmd/flux-schema/testdata/validate/schemas
```

Test the schemas:

```sh
./bin/flux-schema validate \
./cmd/flux-schema/testdata/validate/manifests/valid-*.yaml \
--schema-location './cmd/flux-schema/testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
--skip-missing-schemas \
--verbose
```

Compare with the ecosystem catalog:

```sh
./bin/flux-schema validate \
./cmd/flux-schema/testdata/validate/manifests/valid-*.yaml \
--schema-location ecosystem \
--skip-missing-schemas \
--verbose
```

Compare with kubeconform:

```sh
kubeconform -verbose \
-schema-location './cmd/flux-schema/testdata/validate/schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
./cmd/flux-schema/testdata/validate/manifests/invalid-*.yaml
```

Test invalid manifests:

```sh
./bin/flux-schema validate \
./cmd/flux-schema/testdata/validate/manifests/invalid-*.yaml \
--schema-location './cmd/flux-schema/testdata/validate/schemas/{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
--verbose
```