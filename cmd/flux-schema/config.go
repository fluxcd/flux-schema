// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

// envConfigFile names the environment variable that points at a default
// --config path. The CLI flag wins when both are set.
const envConfigFile = "FLUX_SCHEMA_CONFIG"

func loadConfigFile(path string) (*apiv1.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg apiv1.Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.APIVersion != apiv1.GroupVersion.String() {
		return nil, fmt.Errorf("config %s: unsupported apiVersion %q (want %q)",
			path, cfg.APIVersion, apiv1.GroupVersion.String())
	}
	if cfg.Kind != apiv1.ConfigKind {
		return nil, fmt.Errorf("config %s: unsupported kind %q (want %q)",
			path, cfg.Kind, apiv1.ConfigKind)
	}
	return &cfg, nil
}

// applyValidateConfig copies cfg values into args for flags not set on the CLI,
// giving CLI > config > defaults precedence. Nil cfg is a no-op so callers can
// pass cfg.Validate directly without a nil check. Returns an error when a
// config value fails the same validation the CLI flag would apply (e.g. an
// invalid output format), so bad config is caught up-front rather than
// silently ignored.
func applyValidateConfig(cmd *cobra.Command, cfg *apiv1.ValidateConfig, args *validateFlags) error {
	if cfg == nil {
		return nil
	}
	flags := cmd.Flags()

	if cfg.SchemaLocations != nil && !flags.Changed("schema-location") {
		args.schemaLocations = cfg.SchemaLocations
	}
	if !flags.Changed("skip-missing-schemas") {
		args.skipMissingSchemas = cfg.SkipMissingSchemas
	}
	if cfg.SkipKinds != nil && !flags.Changed("skip-kind") {
		args.skipKinds = cfg.SkipKinds
	}
	if cfg.SkipJSONPaths != nil && !flags.Changed("skip-json-path") {
		args.skipJSONPaths = cfg.SkipJSONPaths
	}
	if cfg.SkipFiles != nil && !flags.Changed("skip-file") {
		args.skipFiles = cfg.SkipFiles
	}
	if !flags.Changed("skip-cel-rules") {
		args.skipCELRules = cfg.SkipCELRules
	}
	if !flags.Changed("verbose") {
		args.verbose = cfg.Verbose
	}
	if !flags.Changed("fail-fast") {
		args.failFast = cfg.FailFast
	}
	if cfg.Concurrent != nil && !flags.Changed("concurrent") {
		args.concurrent = *cfg.Concurrent
	}
	if !flags.Changed("insecure-skip-tls-verify") {
		args.insecureSkipTLSVerify = cfg.InsecureSkipTLSVerify
	}
	if cfg.Output != "" && !flags.Changed("output") {
		if err := args.output.Set(string(cfg.Output)); err != nil {
			return fmt.Errorf("config output: %w", err)
		}
	}
	return nil
}
