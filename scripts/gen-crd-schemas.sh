#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

usage() {
  echo "Usage: $(basename "$0") -r <owner/repo> -d <directory> [-v <version>] [-p <overlay-path>] [-i <source-name>]"
  echo ""
  echo "Extracts JSON schemas from CRDs under <repo>/<overlay-path> at the given version."
  echo ""
  echo "Options:"
  echo "  -r  GitHub repository in 'owner/name' form"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -v  Repository release tag; defaults to the latest release"
  echo "  -p  Kustomize overlay path within the repository; defaults to 'config/crd'"
  echo "  -i  Also write .fields.txt field indexes, recording the given source"
  echo "      name with the version and repository URL in their headers"
  echo "  -h  Show this help message"
  exit 1
}

repo=""
dir=""
version=""
overlay_path="config/crd"
index_name=""

while getopts "r:d:v:p:i:h" opt; do
  case $opt in
    r) repo="$OPTARG" ;;
    d) dir="$OPTARG" ;;
    v) version="$OPTARG" ;;
    p) overlay_path="$OPTARG" ;;
    i) index_name="$OPTARG" ;;
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

echo "Extracting schemas for ${repo}@${version} (overlay: ${overlay_path}) into ${dir}"
mkdir -p "$dir"

extract_args=(
  -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json'
  --strip-description
  -d "$dir"
)
if [[ -n "$index_name" ]]; then
  extract_args+=(--with-field-index --index-source "${index_name} ${version} https://github.com/${repo}")
fi

kubectl kustomize "https://github.com/${repo}/${overlay_path}?ref=${version}" | \
  flux-schema extract crd /dev/stdin "${extract_args[@]}"

if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_ENV:-}" ]]; then
  prefix=$(basename "$repo" | tr '[:lower:]' '[:upper:]' | tr '-' '_')
  {
    echo "${prefix}_REPO=${repo}"
    echo "${prefix}_VERSION=${version}"
  } >> "$GITHUB_ENV"
fi
