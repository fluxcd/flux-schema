// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	VERSION = "0.0.0-dev.0"
)

var rootCmd = &cobra.Command{
	Use:               "flux-schema",
	Version:           VERSION,
	SilenceUsage:      true,
	SilenceErrors:     true,
	DisableAutoGenTag: true,
	Long: `Flux CLI plugin for Kubernetes schema extraction and manifests validation.
⚠️ Please note that this plugin is in preview and under development.
While we try our best to not introduce breaking changes, they may occur when
we adapt to new features and/or find better ways to facilitate what it does.`,
}

type rootFlags struct {
	timeout time.Duration
}

var rootArgs = rootFlags{
	timeout: time.Minute,
}

func init() {
	rootCmd.PersistentFlags().DurationVar(&rootArgs.timeout, "timeout", rootArgs.timeout,
		"The length of time to wait before giving up on the current operation.")

	rootCmd.SetOut(os.Stdout)
}

func userAgent() string {
	return "flux-schema/" + VERSION
}

// errSilent signals a non-zero exit without printing the usual "✗ ..." line.
// Used by commands that have already emitted a self-describing summary
// (e.g. `validate` prints its own "Summary: ... Invalid: N" line); restating
// the failure on a second line is noise.
var errSilent = errors.New("")

func main() {
	if err := rootCmd.Execute(); err != nil {
		if err.Error() != "" {
			rootCmd.PrintErrf("✗ %v\n", err)
		}
		os.Exit(1)
	}
}
