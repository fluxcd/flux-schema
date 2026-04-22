#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

usage() {
  echo "Usage: $(basename "$0") -r <owner/repo> -d <directory> [-v <version>]"
  echo ""
  echo "Extracts JSON schemas from CRDs under <repo>/config/crd at the given version."
  echo ""
  echo "Options:"
  echo "  -r  GitHub repository in 'owner/name' form"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -v  Repository release tag; defaults to the latest release"
  echo "  -h  Show this help message"
  exit 1
}

repo=""
dir=""
version=""

while getopts "r:d:v:h" opt; do
  case $opt in
    r) repo="$OPTARG" ;;
    d) dir="$OPTARG" ;;
    v) version="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done

if [[ -z "$repo" ]]; then
  echo "Error: repository is required"
  usage
fi

if [[ "$repo" != */* ]]; then
  echo "Error: repository must be in 'owner/name' form"
  exit 1
fi

if [[ -z "$dir" ]]; then
  echo "Error: directory is required"
  usage
fi

if ! command -v kubectl &> /dev/null; then
  echo "Error: kubectl is required but not found"
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
  echo "Discovering latest release of ${repo}..."
  version=$(gh release view --repo "${repo}" --json tagName -q .tagName)

  if [[ -z "$version" ]]; then
    echo "Error: failed to discover latest version"
    exit 1
  fi
fi

if [[ "$version" != v* ]]; then
  version="v${version}"
fi

echo "Extracting schemas for ${repo}@${version} into ${dir}"
mkdir -p "$dir"
kubectl kustomize "https://github.com/${repo}/config/crd?ref=${version}" | \
  flux-schema extract crd /dev/stdin \
    -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
    -d "$dir"

if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_ENV:-}" ]]; then
  prefix=$(basename "$repo" | tr '[:lower:]' '[:upper:]' | tr '-' '_')
  {
    echo "${prefix}_REPO=${repo}"
    echo "${prefix}_VERSION=${version}"
  } >> "$GITHUB_ENV"
fi
