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

// envConfigFile names the environment variable used when --config is unset.
// The executable-adjacent default path is consulted only when both are unset.
const envConfigFile = "FLUX_SCHEMA_CONFIG"

// configDefaultPlaceholder marks where the executable-derived default config
// path is templated into command Long help text at startup.
const configDefaultPlaceholder = "{{CONFIG_DEFAULT}}"

var executablePath = os.Executable

// defaultConfigDescription returns the executable-derived default config path
// for help text, resolved at startup, or a generic placeholder when the
// executable path cannot be determined.
func defaultConfigDescription() string {
	if exe, err := executablePath(); err == nil {
		return exe + ".config"
	}
	return "<executable>.config"
}

// configFlagUsage is the shared explain --config flag help, with the
// executable-derived default resolved at startup.
func configFlagUsage() string {
	return "path to a YAML config file supplying default flag values " +
		"(default $" + envConfigFile + ", else " + defaultConfigDescription() + ")"
}

// resolveRequiredConfigFile picks the config path for commands whose config
// defaults to the executable-adjacent file, matching flux-mirror login/secret.
func resolveRequiredConfigFile(flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if envPath := os.Getenv(envConfigFile); envPath != "" {
		return envPath, nil
	}
	exe, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("resolve default config path: %w", err)
	}
	return exe + ".config", nil
}

func resolveConfigFile(flagPath string) (string, bool, error) {
	if flagPath != "" {
		return flagPath, true, nil
	}
	if envPath := os.Getenv(envConfigFile); envPath != "" {
		return envPath, true, nil
	}
	exe, err := executablePath()
	if err != nil {
		return "", false, fmt.Errorf("resolve executable path: %w", err)
	}
	path := exe + ".config"
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}
	return path, true, nil
}

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

// loadExplainConfig applies a config file resolved from --config,
// FLUX_SCHEMA_CONFIG, or the executable-adjacent default path. When the user
// passes --schema-location and no config source is set, the default config is
// skipped so explicit catalog selection works without a sidecar config file.
func loadExplainConfig(cmd *cobra.Command) error {
	flags := cmd.Flags()
	if flags.Changed("schema-location") && !flags.Changed("config") && os.Getenv(envConfigFile) == "" {
		return nil
	}
	configPath, err := resolveRequiredConfigFile(explainArgs.configFile)
	if err != nil {
		return err
	}
	cfg, err := loadConfigFile(configPath)
	if err != nil {
		return err
	}
	return applyExplainConfig(cmd, &cfg.Explain, &explainArgs)
}

// applyExplainConfig copies cfg values into args for flags not set on the CLI,
// giving CLI > config > defaults precedence.
func applyExplainConfig(cmd *cobra.Command, cfg *apiv1.ExplainConfig, args *explainFlags) error {
	if cfg == nil {
		return nil
	}
	flags := cmd.Flags()

	if cfg.SchemaLocations != nil && !flags.Changed("schema-location") {
		args.schemaLocations = cfg.SchemaLocations
	}
	if cfg.APIVersion != "" && !flags.Changed("api-version") {
		args.apiVersion = cfg.APIVersion
	}
	if !flags.Changed("recursive") {
		args.recursive = cfg.Recursive
	}
	if !flags.Changed("insecure-skip-tls-verify") {
		args.insecureSkipTLSVerify = cfg.InsecureSkipTLSVerify
	}
	if cfg.Output != "" && !flags.Changed("output") {
		if err := args.output.Set(string(cfg.Output)); err != nil {
			return fmt.Errorf("config explain.output: %w", err)
		}
	}
	return nil
}
