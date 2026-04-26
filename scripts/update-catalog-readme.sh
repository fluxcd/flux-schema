#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o pipefail

usage() {
  echo "Usage: $(basename "$0") -f <readme>"
  echo ""
  echo "Rewrites the versions table between '<!-- versions:start -->' and"
  echo "'<!-- versions:end -->' markers using the K8S_REPO/K8S_VERSION,"
  echo "GATEWAY_API_REPO/GATEWAY_API_VERSION, FLUX_REPO/FLUX_VERSION,"
  echo "FLUX_OPERATOR_REPO/FLUX_OPERATOR_VERSION, FLAGGER_REPO/FLAGGER_VERSION"
  echo "and OPENSHIFT_REPO/OPENSHIFT_VERSION env vars."
  echo ""
  echo "Options:"
  echo "  -f  Path to the README file to update"
  echo "  -h  Show this help message"
  exit 1
}

readme=""

while getopts "f:h" opt; do
  case $opt in
    f) readme="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done

if [[ -z "$readme" ]]; then
  echo "Error: readme path is required"
  usage
fi

if [[ ! -f "$readme" ]]; then
  echo "Error: $readme is not a file"
  exit 1
fi

for v in K8S_REPO K8S_VERSION GATEWAY_API_REPO GATEWAY_API_VERSION FLUX_REPO FLUX_VERSION FLUX_OPERATOR_REPO FLUX_OPERATOR_VERSION FLAGGER_REPO FLAGGER_VERSION OPENSHIFT_REPO OPENSHIFT_VERSION; do
  if [[ -z "${!v:-}" ]]; then
    echo "Error: ${v} is not set"
    exit 1
  fi
done

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

awk \
  -v k8s_repo="$K8S_REPO" -v k8s_ver="$K8S_VERSION" \
  -v gw_repo="$GATEWAY_API_REPO" -v gw_ver="$GATEWAY_API_VERSION" \
  -v flux_repo="$FLUX_REPO" -v flux_ver="$FLUX_VERSION" \
  -v fop_repo="$FLUX_OPERATOR_REPO" -v fop_ver="$FLUX_OPERATOR_VERSION" \
  -v flagger_repo="$FLAGGER_REPO" -v flagger_ver="$FLAGGER_VERSION" \
  -v os_repo="$OPENSHIFT_REPO" -v os_ver="$OPENSHIFT_VERSION" '
  /<!-- versions:start -->/ {
    print
    print "| Source | Version |"
    print "| --- | --- |"
    printf("| [%s](https://github.com/%s) | %s |\n", k8s_repo, k8s_repo, k8s_ver)
    printf("| [%s](https://github.com/%s) | %s |\n", gw_repo, gw_repo, gw_ver)
    printf("| [%s](https://github.com/%s) | %s |\n", os_repo, os_repo, os_ver)
    printf("| [%s](https://github.com/%s) | %s |\n", flux_repo, flux_repo, flux_ver)
    printf("| [%s](https://github.com/%s) | %s |\n", flagger_repo, flagger_repo, flagger_ver)
    printf("| [%s](https://github.com/%s) | %s |\n", fop_repo, fop_repo, fop_ver)
    skip = 1
    next
  }
  /<!-- versions:end -->/ { skip = 0 }
  !skip { print }
' "$readme" > "$tmp"

mv "$tmp" "$readme"
trap - EXIT

echo "Updated versions table in ${readme}"
