#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

OPENSHIFT_REPO="openshift/api"
ENDOFLIFE_URL="https://endoflife.date/api/v1/products/red-hat-openshift/"

usage() {
  echo "Usage: $(basename "$0") -d <directory> [-r <ref>]"
  echo ""
  echo "Extracts JSON schemas from the openshift/api OpenAPI v2 swagger at the"
  echo "given git ref. Without -r, resolves the latest non-EOL OpenShift minor"
  echo "release via the endoflife.date API and uses the matching release-X.Y"
  echo "branch."
  echo ""
  echo "Options:"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -r  openshift/api git ref (e.g. release-4.20); defaults to the latest"
  echo "      shipping release branch resolved via endoflife.date"
  echo "  -h  Show this help message"
  exit 1
}

dir=""
ref=""

while getopts "d:r:h" opt; do
  case $opt in
    d) dir="$OPTARG" ;;
    r) ref="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done

if [[ -z "$dir" ]]; then
  echo "Error: directory is required"
  usage
fi

if ! command -v curl &> /dev/null; then
  echo "Error: curl is required but not found"
  exit 1
fi

if ! command -v jq &> /dev/null; then
  echo "Error: jq is required but not found"
  exit 1
fi

if ! command -v flux-schema &> /dev/null; then
  echo "Error: flux-schema is required but not found in PATH"
  exit 1
fi

version=""

if [[ -z "$ref" ]]; then
  echo "Discovering latest OpenShift release from ${ENDOFLIFE_URL}..."
  # Sort by parsed (major, minor) descending; do not rely on the API's
  # implicit ordering. Strip any trailing patch component defensively
  # — the openshift/api repo names branches release-X.Y, never
  # release-X.Y.Z.
  version=$(curl -fsSL "$ENDOFLIFE_URL" \
    | jq -r '
        [.result.releases[]
          | select(.isEol == false)
          | .name]
        | map(. as $n | (split(".") | map(tonumber)) as $p | {n: $n, p: $p})
        | sort_by(.p)
        | reverse
        | .[0].n // empty')
  if [[ -z "$version" ]]; then
    echo "Error: could not resolve latest OpenShift release from endoflife.date"
    exit 1
  fi
  if [[ ! "$version" =~ ^([0-9]+\.[0-9]+) ]]; then
    echo "Error: endoflife.date returned malformed version ${version}"
    exit 1
  fi
  # Capture only the X.Y prefix so we compose a valid release branch
  # (openshift/api branches are release-X.Y, never release-X.Y.Z).
  version="${BASH_REMATCH[1]}"
  ref="release-${version}"
else
  # Manual -r: surface a clean version for the README. Strip the
  # release- prefix when present; otherwise use the ref verbatim
  # (covers tags and SHAs).
  version="${ref#release-}"
fi

# Match the v-prefix convention used by the other catalog generators
# (K8S_VERSION=v1.35.4, FLUX_VERSION=v2.8.6, …). Only prefix when the
# value parses as numeric X.Y[.Z]; tags and SHAs pass through.
if [[ "$version" =~ ^[0-9]+\.[0-9]+ ]]; then
  version="v${version}"
fi

mkdir -p "$dir"

echo "Extracting schemas for ${OPENSHIFT_REPO}@${ref} into ${dir}"

flux-schema extract openshift --ref "${ref}" \
  -f '{{ .Group }}/{{ .Kind }}_{{ .Version }}.json' \
  --strip-description \
  -d "${dir}"

if [[ "${GITHUB_ACTIONS:-}" == "true" && -n "${GITHUB_ENV:-}" ]]; then
  {
    echo "OPENSHIFT_REPO=${OPENSHIFT_REPO}"
    echo "OPENSHIFT_VERSION=${version}"
  } >> "$GITHUB_ENV"
fi
