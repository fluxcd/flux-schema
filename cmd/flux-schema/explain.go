// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	explainer "github.com/fluxcd/flux-schema/internal/explain"
	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/validator"
)

var explainCmd = &cobra.Command{
	Use:                   "explain TYPE [--recursive=FALSE|TRUE] [--api-version=api-version-group] [-o|--output=plaintext|plaintext-openapiv2]",
	DisableFlagsInUseLine: true,
	Short:                 "Get documentation for a resource",
	Long: `Describe fields and structure of various resources.

 This command describes the fields associated with each supported API resource. Fields are identified via a simple JSONPath identifier:

        <type>.<fieldName>[.<fieldName>]

 Information about each field is retrieved from schema catalogs in JSON Schema format. Catalogs are selected with --schema-location, or with the explain section in the config file read from --config, else $FLUX_SCHEMA_CONFIG, else
{{CONFIG_DEFAULT}}.`,
	Example: `  # Get the documentation of the resource and its fields
  flux-schema explain pods --schema-location ./catalog

  # Get all the fields in the resource
  flux-schema explain pods --recursive --schema-location ./catalog

  # Get the explanation for deployment in supported api versions
  flux-schema explain deployments --api-version=apps/v1 --schema-location ./catalog

  # Get the documentation of a specific field of a resource
  flux-schema explain pods.spec.containers --schema-location ./catalog

  # Get the documentation of resources in different format
  flux-schema explain deployment --output=plaintext-openapiv2 --schema-location ./catalog`,
	Args:              explainArgsValidate,
	ValidArgsFunction: completeExplainResourceRefs,
	RunE:              explainCmdRun,
}

type explainFlags struct {
	apiVersion            string
	output                flags.ExplainOutput
	recursive             bool
	schemaLocations       []string
	insecureSkipTLSVerify bool
	configFile            string
}

var explainArgs = explainFlags{output: flags.ExplainOutputPlaintext}

func init() {
	explainCmd.Long = strings.ReplaceAll(explainCmd.Long, configDefaultPlaceholder, defaultConfigDescription())
	explainCmd.Flags().BoolVar(&explainArgs.recursive, "recursive", false,
		"Print the fields of fields (Currently only 1 level deep)")
	explainCmd.Flags().StringVar(&explainArgs.apiVersion, "api-version", "",
		"Get different explanations for particular API version (API group/version)")
	explainCmd.Flags().VarP(&explainArgs.output, "output", "o", explainArgs.output.Description())
	explainCmd.Flags().StringArrayVarP(&explainArgs.schemaLocations, "schema-location", "s", nil,
		"URL or file path for schemas (repeatable); 'default' points at the built-in catalog, 'ecosystem' at schemas.fluxoperator.dev")
	explainCmd.Flags().BoolVar(&explainArgs.insecureSkipTLSVerify, "insecure-skip-tls-verify", false,
		"disable TLS certificate verification when fetching schemas over HTTPS")
	explainCmd.Flags().StringVarP(&explainArgs.configFile, "config", "f", "", configFlagUsage())
	_ = explainCmd.MarkFlagFilename("config", "yaml", "yml")
	rootCmd.AddCommand(explainCmd)
}

func explainArgsValidate(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("you must specify the type of resource to explain")
	}
	if len(args) > 1 {
		return fmt.Errorf("we accept only this format: explain RESOURCE")
	}
	return nil
}

func completeExplainResourceRefs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if err := loadExplainConfig(cmd); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	opts, err := buildExplainerOptions()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ex, err := explainer.New(opts)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	matches, err := ex.CompleteReferences(cmd.Context(), toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func explainCmdRun(cmd *cobra.Command, args []string) error {
	if err := loadExplainConfig(cmd); err != nil {
		return err
	}
	opts, err := buildExplainerOptions()
	if err != nil {
		return err
	}
	ex, err := explainer.New(opts)
	if err != nil {
		return err
	}
	return ex.Explain(cmd.Context(), args[0], cmd.OutOrStdout())
}

func buildExplainerOptions() (explainer.Options, error) {
	locations, err := buildExplainSchemaLocations()
	if err != nil {
		return explainer.Options{}, err
	}
	return explainer.Options{
		SchemaLocations:       locations,
		MetadataLocations:     buildExplainMetadataLocations(locations),
		IndexLocations:        buildExplainIndexLocations(locations),
		APIVersion:            explainArgs.apiVersion,
		OutputFormat:          explainArgs.output.String(),
		Recursive:             explainArgs.recursive,
		UserAgent:             userAgent(),
		HTTPTimeout:           rootArgs.timeout,
		InsecureSkipTLSVerify: explainArgs.insecureSkipTLSVerify,
	}, nil
}

func buildExplainSchemaLocations() ([]string, error) {
	if len(explainArgs.schemaLocations) == 0 {
		return nil, fmt.Errorf("no schema locations configured; pass --schema-location or set explain.schemaLocation in the config file")
	}
	return expandSchemaLocations(explainArgs.schemaLocations)
}

func buildExplainMetadataLocations(schemaLocations []string) []string {
	var out []string
	for _, location := range schemaLocations {
		if isEcosystemSchemaLocation(location) {
			continue
		}
		metadataLocation, ok := explainMetadataLocation(location)
		if !ok || containsString(out, metadataLocation) {
			continue
		}
		out = append(out, metadataLocation)
	}
	return out
}

func buildExplainIndexLocations(schemaLocations []string) []string {
	var out []string
	for _, location := range schemaLocations {
		if !isEcosystemSchemaLocation(location) || containsString(out, validator.EcosystemIndexLocation) {
			continue
		}
		out = append(out, validator.EcosystemIndexLocation)
	}
	return out
}

func isEcosystemSchemaLocation(location string) bool {
	base, _ := splitLocationTail(location)
	idx := strings.Index(base, "{{")
	if idx >= 0 {
		base = base[:idx]
	}
	base = strings.TrimRight(base, "/\\")
	return strings.EqualFold(base, validator.EcosystemSchemaBase)
}

func explainMetadataLocation(location string) (string, bool) {
	base, tail := splitLocationTail(location)
	idx := strings.Index(base, "{{")
	if idx < 0 {
		return "", false
	}
	root := strings.TrimRight(base[:idx], `/\`)
	if root == "" {
		return "", false
	}
	return root + "/" + explainer.MetadataDir + tail, true
}

func splitLocationTail(location string) (string, string) {
	if idx := strings.IndexAny(location, "?#"); idx >= 0 {
		return location[:idx], location[idx:]
	}
	return location, ""
}

func containsString(items []string, item string) bool {
	for _, existing := range items {
		if existing == item {
			return true
		}
	}
	return false
}
