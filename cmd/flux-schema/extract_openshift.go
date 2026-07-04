// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/extractor"
	"github.com/fluxcd/flux-schema/internal/flags"
)

const defaultOpenShiftSwaggerURL = "https://raw.githubusercontent.com/openshift/api/%s/openapi/openapi.json"

var extractOpenShiftCmd = &cobra.Command{
	Use:   "openshift [swagger-file]",
	Short: "Extract JSON Schemas from an openshift/api OpenAPI v2 swagger document",
	Example: `  # Fetch the swagger from openshift/api at a git ref
  flux-schema extract openshift --ref release-4.20 -d ./schemas

  # Pipe from stdin
  curl -sL https://raw.githubusercontent.com/openshift/api/release-4.20/openapi/openapi.json \
    | flux-schema extract openshift -d ./schemas`,
	Args: requireFileOrRef,
	RunE: extractOpenShiftCmdRun,
}

type extractOpenShiftFlags struct {
	flags.ExtractOutput
	ref string
}

var extractOpenShiftArgs = extractOpenShiftFlags{
	ExtractOutput: flags.NewExtractOutput(),
}

func init() {
	extractOpenShiftArgs.Register(extractOpenShiftCmd)
	extractOpenShiftCmd.Flags().StringVar(&extractOpenShiftArgs.ref, "ref", "",
		"openshift/api git ref (e.g. release-4.20) to fetch the upstream swagger from")
	extractCmd.AddCommand(extractOpenShiftCmd)
}

// requireFileOrRef enforces mutual exclusion between a single positional
// swagger file (or piped stdin) and --ref. An explicit empty --ref="" is
// rejected here so it never reaches the URL formatter — Cobra would
// otherwise treat it the same as the flag being absent.
func requireFileOrRef(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("accepts at most 1 positional argument, received %d", len(args))
	}
	if cmd.Flags().Changed("ref") && extractOpenShiftArgs.ref == "" {
		return fmt.Errorf("--ref must not be empty")
	}
	hasRef := extractOpenShiftArgs.ref != ""
	switch {
	case hasRef && len(args) == 1:
		return fmt.Errorf("--ref and a swagger file are mutually exclusive")
	case !hasRef && len(args) == 0 && !stdinIsPiped():
		return fmt.Errorf("either a swagger file, piped stdin, or --ref is required")
	}
	return nil
}

func extractOpenShiftCmdRun(cmd *cobra.Command, args []string) error {
	var arg string
	if extractOpenShiftArgs.ref == "" {
		inputs, err := resolveStdinArgs(args)
		if err != nil {
			return err
		}
		arg = inputs[0]
	}
	client := newDefaultK8sHTTPClient()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	source, data, err := resolveOpenShiftInput(ctx, client, defaultOpenShiftSwaggerURL, arg, extractOpenShiftArgs.ref, rootArgs.timeout)
	if err != nil {
		return err
	}

	return runSwaggerExtract(cmd, source, data, extractOpenShiftArgs.ExtractOutput,
		openShiftExtractWithSourceRef(extractOpenShiftArgs.ref))
}

func openShiftExtractWithSourceRef(ref string) func([]byte) ([]extractor.Schema, []error) {
	return func(data []byte) ([]extractor.Schema, []error) {
		schemas, errs := extractor.ExtractOpenShift(data)
		for i := range schemas {
			if schemas[i].Source == "OpenShift" && ref != "" {
				schemas[i].Source = "OpenShift " + ref
			}
		}
		return schemas, errs
	}
}

// resolveOpenShiftInput returns (source, data). source is the file path or
// URL used as the log header. Exactly one of arg / ref is populated by the
// time this runs (requireFileOrRef has already enforced that).
func resolveOpenShiftInput(ctx context.Context, client *retryablehttp.Client,
	urlTemplate, arg, ref string, timeout time.Duration,
) (string, []byte, error) {
	if arg != "" {
		data, err := readSource(arg)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", arg, err)
		}
		return arg, data, nil
	}
	return fetchOpenShiftSwagger(ctx, client, urlTemplate, ref, timeout)
}

// fetchOpenShiftSwagger downloads the openshift/api OpenAPI v2 swagger
// document at the given git ref. The empty-ref guard in requireFileOrRef
// must run before this — an empty ref produces a doubled '/' in the URL
// that raw.githubusercontent.com handles inconsistently.
func fetchOpenShiftSwagger(ctx context.Context, client *retryablehttp.Client,
	urlTemplate, ref string, timeout time.Duration,
) (string, []byte, error) {
	if ref == "" {
		return "", nil, fmt.Errorf("--ref must not be empty")
	}
	url := fmt.Sprintf(urlTemplate, ref)
	body, err := fetchURL(ctx, client, url, timeout)
	return url, body, err
}
