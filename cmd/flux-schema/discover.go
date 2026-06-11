// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/inventory"
)

var discoverCmd = &cobra.Command{
	Use:   "discover [path]",
	Short: "Discover Flux and Kubernetes resources in a GitOps repository",
	Example: `  # Inventory the current directory as JSON
  # Flux resources are listed per file, Kubernetes resources are counted
  # by kind, and every directory is classified as manifests,
  # kustomize-overlay, helm-chart or terraform
  flux-schema discover -o json

  # Inventory a specific directory
  flux-schema discover ./clusters/production

  # Skip files and directories by basename glob
  # default skips dotfiles and dot-directories e.g. '.git'
  flux-schema discover --skip-file '.*' --skip-file 'tests'`,
	Args: cobra.MaximumNArgs(1),
	RunE: discoverCmdRun,
}

type discoverFlags struct {
	skipFiles []string
	output    flags.Output
}

var discoverArgs = discoverFlags{output: "text"}

func init() {
	discoverCmd.Flags().StringArrayVar(&discoverArgs.skipFiles, "skip-file", nil,
		"glob pattern matched against files and dirs "+
			"defaults to skipping dotfiles and dot-dirs (repeatable)")
	discoverCmd.Flags().VarP(&discoverArgs.output, "output", "o", discoverArgs.output.Description())
	rootCmd.AddCommand(discoverCmd)
}

func discoverCmdRun(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) == 1 {
		path = args[0]
	}
	if isStdinSentinel(path) {
		return errors.New("discover does not read from stdin; pass a file or directory path")
	}

	res, err := inventory.Scan(path, inventory.Options{SkipFiles: discoverArgs.skipFiles})
	if err != nil {
		return err
	}

	inv := inventory.NewInventory("flux-schema/"+VERSION, time.Now(), res)
	if mode := discoverArgs.output.String(); mode != "text" {
		return writeInventory(cmd, mode, inv)
	}
	writeInventoryText(cmd, inv.Inventory)
	return nil
}

// writeInventory streams inv to cmd's output as JSON or YAML. The
// `$schema` key is JSON-only: it points at a JSON Schema document and
// carries no meaning for YAML consumers, so we drop it in YAML mode.
func writeInventory(cmd *cobra.Command, mode string, inv apiv1.Inventory) error {
	switch mode {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(inv); err != nil {
			return fmt.Errorf("marshal inventory: %w", err)
		}
		return nil
	case "yaml":
		inv.Schema = ""
		data, err := yaml.Marshal(inv)
		if err != nil {
			return fmt.Errorf("marshal inventory: %w", err)
		}
		cmd.Print(string(data))
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", mode)
	}
}

// writeInventoryText renders the inventory in a human-readable form,
// matching the envelope field order: directory classifications, per-GVK
// resource counts, Flux resources by defining file, and a final summary
// line. Empty sections are omitted.
func writeInventoryText(cmd *cobra.Command, spec apiv1.InventorySpec) {
	if len(spec.Directories) > 0 {
		cmd.Printf("Directories:\n")
		for _, dir := range slices.Sorted(maps.Keys(spec.Directories)) {
			cmd.Printf("  %s: %s\n", dir, spec.Directories[dir])
		}
	}
	if len(spec.Resources) > 0 {
		cmd.Printf("Resources:\n")
		for _, gvk := range slices.Sorted(maps.Keys(spec.Resources)) {
			cmd.Printf("  %s: %d\n", gvk, spec.Resources[gvk])
		}
	}
	if len(spec.Flux) > 0 {
		cmd.Printf("Flux:\n")
		for _, kind := range slices.Sorted(maps.Keys(spec.Flux)) {
			cmd.Printf("  %s:\n", kind)
			for _, source := range slices.Sorted(maps.Keys(spec.Flux[kind])) {
				cmd.Printf("    %s: %s\n", source, strings.Join(spec.Flux[kind][source], ", "))
			}
		}
	}
	resources := pluralize("resource", spec.Summary.Resources)
	files := pluralize("file", spec.Summary.Files)
	cmd.Printf("Summary: %d %s found in %d %s with %d lines of YAML\n",
		spec.Summary.Resources, resources, spec.Summary.Files, files,
		spec.Summary.LinesOfYAML)
}
