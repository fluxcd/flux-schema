#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

K8S_REPO="kubernetes/kubernetes"

usage() {
  echo "Usage: $(basename "$0") -d <directory> [-v <version>]"
  echo ""
  echo "Extracts JSON schemas from the Kubernetes OpenAPI v2 swagger at the given version."
  echo ""
  echo "Options:"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -v  Kubernetes release tag (e.g. 1.35.3); defaults to the latest release"
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

if ! command -v gh &> /dev/null; then
  echo "Error: gh CLI is required but not found"
  exit 1
fi

if ! command -v flux-schema &> /dev/null; then
  echo "Error: flux-schema is required but not found in PATH"
  exit 1
fi

if [[ -z "$version" ]]; then
  echo "Discovering latest release of ${K8S_REPO}..."
  version=$(gh release view --repo "${K8S_REPO}" --json tagName -q .tagName)

  if [[ -z "$version" ]]; then
    echo "Error: failed to discover latest version"
    exit 1
  fi
fi

version="${version#v}"

mkdir -p "$dir"

echo "Extracting schemas for ${K8S_REPO}@v${version} into ${dir}"

flux-schema extract k8s --version "${version}" \
  -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
  -d "$dir"

if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_ENV:-}" ]]; then
  {
    echo "K8S_REPO=${K8S_REPO}"
    echo "K8S_VERSION=v${version}"
  } >> "$GITHUB_ENV"
fi
