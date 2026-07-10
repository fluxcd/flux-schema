#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

OPENSHIFT_REPO="openshift/api"
CINCINNATI_URL="https://api.openshift.com/api/upgrades_info/v1/graph"
# The minor the GA probe starts from. Only a floor, not a pin: the probe walks
# stable channels upward from here.
PROBE_FLOOR=20

usage() {
  echo "Usage: $(basename "$0") -d <directory> [-r <ref>]"
  echo ""
  echo "Extracts JSON schemas from the openshift/api OpenAPI v2 swagger at the"
  echo "given git ref. Without -r, resolves the latest GA OpenShift minor"
  echo "release via the Cincinnati upgrade graph API and uses the matching"
  echo "release-X.Y branch."
  echo ""
  echo "Options:"
  echo "  -d  Directory to write the generated JSON schemas to"
  echo "  -r  openshift/api git ref (e.g. release-4.20); defaults to the latest"
  echo "      GA release branch resolved via the Cincinnati graph"
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
  echo "Discovering latest GA OpenShift release from ${CINCINNATI_URL}..."
  # Probe the stable channels of the Cincinnati upgrade graph (the data
  # OpenShift clusters themselves upgrade from) upward from the floor. A
  # minor is GA once its stable channel carries a release of that minor;
  # stable channels also list previous-minor releases as upgrade sources,
  # so the node versions are matched against the minor itself. Pre-GA and
  # unknown channels answer 200 with an empty node list, ending the walk.
  minor=$PROBE_FLOOR
  while :; do
    count=$(curl -fsSL -H 'Accept: application/json' \
      "${CINCINNATI_URL}?channel=stable-4.${minor}" \
      | jq --arg p "4.${minor}." '[.nodes[]? | select(.version | startswith($p))] | length')
    if [[ "$count" -eq 0 ]]; then
      break
    fi
    version="4.${minor}"
    minor=$((minor + 1))
  done
  if [[ -z "$version" ]]; then
    echo "Error: could not resolve latest OpenShift release: stable-4.${PROBE_FLOOR} has no GA release"
    exit 1
  fi
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
