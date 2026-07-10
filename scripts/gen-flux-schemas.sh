#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

FLUX_REPO="fluxcd/flux2"

usage() {
  echo "Usage: $(basename "$0") -d <directory> [-v <version>]"
  echo ""
  echo "Generates a FluxInstance manifest at the given Flux version, builds it"
  echo "with flux-operator and extracts JSON schemas with .fields.txt field"
  echo "indexes from the resulting CRDs."
  echo ""
  echo "Options:"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -v  Flux release tag (e.g. v2.5.0); defaults to the latest release"
  echo "  -h  Show this help message"
  exit 1
}

dir=""
version=""

while getopts "d:v:h" opt; do
  case $opt in
    d) dir="$OPTARG" ;;
    v) version="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done

if [[ -z "$dir" ]]; then
  echo "Error: directory is required"
  usage
fi

if ! command -v flux-operator &> /dev/null; then
  echo "Error: flux-operator is required but not found"
  exit 1
fi

if ! command -v gh &> /dev/null; then
  echo "Error: gh CLI is required but not found"
  exit 1
fi

if ! command -v flux-schema &> /dev/null; then
  echo "Error: flux-schema is required but not found in PATH"
  exit 1
fi

if [[ -z "$version" ]]; then
  echo "Discovering latest release of ${FLUX_REPO}..."
  version=$(gh release view --repo "${FLUX_REPO}" --json tagName -q .tagName)

  if [[ -z "$version" ]]; then
    echo "Error: failed to discover latest version"
    exit 1
  fi
fi

if [[ "$version" != v* ]]; then
  version="v${version}"
fi

mkdir -p "$dir"

echo "Extracting schemas for ${FLUX_REPO}@${version} into ${dir}"

cat <<EOF | flux-operator build instance -f - | \
  flux-schema extract crd /dev/stdin \
    -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
    --strip-description \
    --with-field-index \
    --index-source "Flux ${version} https://github.com/${FLUX_REPO}" \
    -d "$dir"
apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "${version}"
    registry: "ghcr.io/fluxcd"
  components:
    - source-controller
    - source-watcher
    - kustomize-controller
    - helm-controller
    - notification-controller
    - image-reflector-controller
    - image-automation-controller
EOF

if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_ENV:-}" ]]; then
  {
    echo "FLUX_REPO=${FLUX_REPO}"
    echo "FLUX_VERSION=${version}"
  } >> "$GITHUB_ENV"
fi
