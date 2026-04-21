// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/spf13/cobra"
)

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract JSON Schemas from Kubernetes API sources",
}

func init() {
	rootCmd.AddCommand(extractCmd)
}
