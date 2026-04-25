// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const supportedConfigVersion = "1"

// envConfigFile names the environment variable that points at a default
// --config path. The CLI flag wins when both are set.
const envConfigFile = "FLUX_SCHEMA_CONFIG"

type configFile struct {
	Version  string          `json:"version"`
	Validate *validateConfig `json:"validate,omitempty"`
}

type validateConfig struct {
	SchemaLocations       []string `json:"schema-location,omitempty"`
	SkipMissingSchemas    bool     `json:"skip-missing-schemas,omitempty"`
	SkipKinds             []string `json:"skip-kind,omitempty"`
	SkipJSONPaths         []string `json:"skip-json-path,omitempty"`
	Verbose               bool     `json:"verbose,omitempty"`
	FailFast              bool     `json:"fail-fast,omitempty"`
	Concurrent            *int     `json:"concurrent,omitempty"`
	InsecureSkipTLSVerify bool     `json:"insecure-skip-tls-verify,omitempty"`
	Output                string   `json:"output,omitempty"`
}

func loadConfigFile(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg configFile
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Version != supportedConfigVersion {
		return nil, fmt.Errorf("config %s: unsupported version %q (want %q)",
			path, cfg.Version, supportedConfigVersion)
	}
	return &cfg, nil
}

// applyValidateConfig copies cfg values into args for flags not set on the CLI,
// giving CLI > config > defaults precedence. Nil cfg is a no-op so callers can
// pass cfg.Validate directly without a nil check. Returns an error when a
// config value fails the same validation the CLI flag would apply (e.g. an
// invalid output format), so bad config is caught up-front rather than
// silently ignored.
func applyValidateConfig(cmd *cobra.Command, cfg *validateConfig, args *validateFlags) error {
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
		if err := args.output.Set(cfg.Output); err != nil {
			return fmt.Errorf("config output: %w", err)
		}
	}
	return nil
}
