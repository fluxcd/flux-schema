#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# This script validates Kubernetes manifests using the Flux Schema CLI.
# It builds kustomize overlays and validating the output against the Flux schema catalog.
# This script is meant to be run locally and in CI before the changes
# are merged on the main branch that's synced by Flux.

# Prerequisites
# - kubectl >= 1.36
# - flux-schema >= 0.1

# Usage example:
#   validate.sh \
#     -d ./manifests \
#     -s default \
#     -s ./my-schemas \
#     -s https://raw.githubusercontent.com/datreeio/CRDs-catalog/main

set -o errexit
set -o pipefail

# mirror kustomize-controller build options
kustomize_flags=("--load-restrictor=LoadRestrictionsNone")
kustomize_config="kustomization.yaml"

# Strip SOPS-encrypted fields before validation (Flux removes these at apply
# time) and skip documents whose schema is not in the catalog.
flux_schema_flags=("--skip-json-path=/sops" "--skip-missing-schemas" "--verbose")

# root directory to validate
root_dir="."

# directories to exclude from validation
exclude_dirs=()

# extra --schema-location values to pass through to flux-schema (repeatable)
schema_locations=()

# directories auto-detected as non-Kubernetes (terraform, helm charts)
declare -a auto_skip_dirs=()

# directories that are kustomize overlays
declare -a kustomize_dirs=()

usage() {
  echo "Usage: $0 [-d <dir>] [-e <dir>]... [-s <location>]... [-h]"
  echo ""
  echo "Validate Flux custom resources and kustomize overlays using flux-schema."
  echo ""
  echo "Options:"
  echo "  -d, --dir <dir>                  Root directory to validate (default: current directory)"
  echo "  -e, --exclude <dir>              Directory to exclude from validation (can be repeated)"
  echo "  -s, --schema-location <location> Schema URL or file path passed to flux-schema; 'default'"
  echo "                                   selects the built-in catalog (can be repeated; tried in order)"
  echo "  -h, --help                       Show this help message"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -d|--dir)
        if [[ -z "${2:-}" ]]; then
          echo "ERROR - --dir requires a directory argument" >&2
          exit 1
        fi
        root_dir="${2%/}"
        shift 2
        ;;
      -e|--exclude)
        if [[ -z "${2:-}" ]]; then
          echo "ERROR - --exclude requires a directory argument" >&2
          exit 1
        fi
        exclude_dirs+=("./${2#./}")
        shift 2
        ;;
      -s|--schema-location)
        if [[ -z "${2:-}" ]]; then
          echo "ERROR - --schema-location requires a URL or file path argument" >&2
          exit 1
        fi
        schema_locations+=("--schema-location=$2")
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "ERROR - Unknown argument: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
  done
}

check_prerequisites() {
  local missing=0
  for cmd in kubectl flux-schema; do
    if ! command -v "$cmd" &> /dev/null; then
      echo "ERROR - $cmd is not installed" >&2
      missing=1
    fi
  done
  if [[ $missing -ne 0 ]]; then
    exit 1
  fi
}

# Normalize a path by stripping leading "./" for consistent comparisons
normalize_path() {
  local p="${1#./}"
  echo "${p%/}"
}

# Check if a path is under a user-excluded, auto-skipped, or kustomize directory
is_excluded_dir() {
  local path
  path="$(normalize_path "$1")"
  for dir in "${exclude_dirs[@]}"; do
    local d
    d="$(normalize_path "$dir")"
    if [[ "$path" == "$d"/* || "$path" == "$d" ]]; then
      return 0
    fi
  done
  for dir in "${auto_skip_dirs[@]}"; do
    local d
    d="$(normalize_path "$dir")"
    if [[ "$path" == "$d"/* || "$path" == "$d" ]]; then
      return 0
    fi
  done
  for dir in "${kustomize_dirs[@]}"; do
    local d
    d="$(normalize_path "$dir")"
    if [[ "$path" == "$d"/* || "$path" == "$d" ]]; then
      return 0
    fi
  done
  return 1
}

# Check if a path is under a user-excluded or auto-skipped directory (but not kustomize dirs)
is_non_kustomize_excluded_dir() {
  local path
  path="$(normalize_path "$1")"
  for dir in "${exclude_dirs[@]}" "${auto_skip_dirs[@]}"; do
    local d
    d="$(normalize_path "$dir")"
    if [[ "$path" == "$d"/* || "$path" == "$d" ]]; then
      return 0
    fi
  done
  return 1
}

# Detect directories containing Terraform files, Helm charts, or kustomize overlays
detect_excluded_dirs() {
  while IFS= read -r -d $'\0' file; do
    auto_skip_dirs+=("$(dirname "$file")")
  done < <(find "$root_dir" -path '*/.*' -prune -o -type f \( -name '*.tf' -o -name 'Chart.yaml' \) -print0)

  while IFS= read -r -d $'\0' file; do
    kustomize_dirs+=("$(dirname "$file")")
  done < <(find "$root_dir" -path '*/.*' -prune -o -type f -name "$kustomize_config" -print0)
}

validate_kubernetes_manifests() {
  echo "INFO - Validating Kubernetes manifests"
  local files=()
  while IFS= read -r -d $'\0' file; do
    dir="$(dirname "$file")"
    if is_excluded_dir "$dir"; then
      continue
    fi
    files+=("$file")
  done < <(find "$root_dir" -path '*/.*' -prune -o -type f -name '*.yaml' -print0)
  if [[ ${#files[@]} -gt 0 ]]; then
    flux-schema validate "${flux_schema_flags[@]}" "${schema_locations[@]}" "${files[@]}"
  fi
}

validate_kustomize_overlays() {
  while IFS= read -r -d $'\0' file; do
    dir="$(dirname "$file")"
    if is_non_kustomize_excluded_dir "$dir"; then
      continue
    fi
    echo "INFO - Validating kustomize overlay ${file/%$kustomize_config}"
    kubectl kustomize "${file/%$kustomize_config}" "${kustomize_flags[@]}" | \
      flux-schema validate "${flux_schema_flags[@]}" "${schema_locations[@]}"
    if [[ ${PIPESTATUS[0]} != 0 || ${PIPESTATUS[1]} != 0 ]]; then
      exit 1
    fi
  done < <(find "$root_dir" -path '*/.*' -prune -o -type f -name "$kustomize_config" -print0)
}

# Main
parse_args "$@"
check_prerequisites
detect_excluded_dirs
validate_kubernetes_manifests
validate_kustomize_overlays
echo "INFO - All validations passed"
