#!/usr/bin/env bash

# Copyright 2026 The Flux authors. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

# This script validates Kubernetes manifests using the Flux Schema CLI.
# It builds kustomize overlays and validating the output against the
# default schema catalog or a user-provided config file.
# The script auto-detects and excludes non-Kubernetes directories such as
# dotfiles, Terraform modules and Helm charts.
# This script is meant to be run locally and in CI before the changes
# are merged on the main branch that's synced by Flux.

# Prerequisites
# - kubectl >= 1.36
# - flux-schema >= 0.2

# Usage example:
#   validate.sh \
#     -d ./manifests \
#     -c ./.fluxschema.yml

set -o errexit
set -o pipefail

# mirror kustomize-controller build options
kustomize_flags=("--load-restrictor=LoadRestrictionsNone")
kustomize_config="kustomization.yaml"

# Default flags used when no config file is found. Strip SOPS-encrypted
# fields before validation (Flux removes these at apply time) and skip
# documents whose schema is not in the catalog.
default_flux_schema_flags=("--skip-json-path=/sops" "--skip-missing-schemas" "--verbose")

# Effective flags passed to flux-schema, populated by resolve_config.
flux_schema_flags=()

# Effective flux-schema invocation, populated by resolve_cli.
# Either ("flux-schema") for the standalone CLI or ("flux" "schema") for the
# Flux CLI plugin.
flux_schema_cmd=()

# root directory to validate
root_dir="."

# path to the flux-schema config file
config_file=".fluxschema.yml"

# directories to exclude from validation
exclude_dirs=()

# directories auto-detected as non-Kubernetes (terraform, helm charts)
declare -a auto_skip_dirs=()

# directories that are kustomize overlays
declare -a kustomize_dirs=()

usage() {
  echo "Usage: $0 [-d <dir>] [-c <file>] [-e <dir>]... [-h]"
  echo ""
  echo "Validate Flux custom resources and kustomize overlays using flux-schema."
  echo ""
  echo "Options:"
  echo "  -d, --dir <dir>      Root directory to validate (default: current directory)"
  echo "  -c, --config <file>  Path to a flux-schema config file (default: .fluxschema.yml)."
  echo "                       When the file does not exist, sensible defaults are used."
  echo "  -e, --exclude <dir>  Directory to exclude from validation (can be repeated)"
  echo "  -h, --help           Show this help message"
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
      -c|--config)
        if [[ -z "${2:-}" ]]; then
          echo "ERROR - --config requires a file path argument" >&2
          exit 1
        fi
        config_file="$2"
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
  if ! command -v kubectl &> /dev/null; then
    echo "ERROR - kubectl is not installed" >&2
    exit 1
  fi
}

# Pick the flux-schema invocation. Prefer the standalone CLI; fall back to
# the Flux CLI plugin form ('flux schema') when only that is available.
resolve_cli() {
  if command -v flux-schema &> /dev/null; then
    flux_schema_cmd=("flux-schema")
  elif command -v flux &> /dev/null && flux schema --help &> /dev/null; then
    flux_schema_cmd=("flux" "schema")
  else
    echo "ERROR - flux-schema is not installed (tried 'flux-schema' and 'flux schema' plugin)" >&2
    exit 1
  fi
}

# Pick the flags to pass to flux-schema. When the config file exists, defer
# all validation options to it; otherwise fall back to the built-in defaults.
resolve_config() {
  if [[ -f "$config_file" ]]; then
    echo "INFO - Using flux-schema config: $config_file"
    flux_schema_flags=("--config=$config_file")
  else
    echo "INFO - Config file '$config_file' not found, using default flags"
    flux_schema_flags=("${default_flux_schema_flags[@]}")
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
    "${flux_schema_cmd[@]}" validate "${flux_schema_flags[@]}" "${files[@]}"
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
      "${flux_schema_cmd[@]}" validate "${flux_schema_flags[@]}"
    if [[ ${PIPESTATUS[0]} != 0 || ${PIPESTATUS[1]} != 0 ]]; then
      exit 1
    fi
  done < <(find "$root_dir" -path '*/.*' -prune -o -type f -name "$kustomize_config" -print0)
}

# Main
parse_args "$@"
check_prerequisites
resolve_cli
resolve_config
detect_excluded_dirs
validate_kubernetes_manifests
validate_kustomize_overlays
echo "INFO - All validations passed"
