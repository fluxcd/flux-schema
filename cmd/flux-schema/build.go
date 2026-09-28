// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fluxcd/flux-schema/internal/source"
	"github.com/fluxcd/flux-schema/internal/substitute"
	"github.com/fluxcd/flux-schema/internal/validator"
)

var buildCmd = &cobra.Command{
	Use:   "build [paths...]",
	Short: "Render Kubernetes manifests as a YAML stream",
	Example: `  # Render plain YAML files and build kustomize directories in a tree
  flux-schema build ./clusters/production ./infrastructure/sources.yaml

  # Apply Flux post-build substitution
  flux-schema build ./clusters/production --envsubst-file .env --envsubst-strict

  # Validate the rendered stream
  flux-schema build ./manifests | flux-schema validate -`,
	RunE: buildCmdRun,
}

type buildFlags struct {
	envsubstFile   string
	envsubstStrict bool
	skipFiles      []string
}

var buildArgs buildFlags

func init() {
	buildCmd.Flags().StringVar(&buildArgs.envsubstFile, "envsubst-file", "",
		"path to a dotenv file supplying Flux post-build substitution variables")
	_ = buildCmd.MarkFlagFilename("envsubst-file")
	buildCmd.Flags().BoolVar(&buildArgs.envsubstStrict, "envsubst-strict", false,
		"fail on undefined substitution variables that have no default")
	buildCmd.Flags().StringArrayVar(&buildArgs.skipFiles, "skip-file", nil,
		"glob pattern matched against files and dirs "+
			"defaults to skipping dotfiles and dot-dirs (repeatable)")
	rootCmd.AddCommand(buildCmd)
}

func buildCmdRun(cmd *cobra.Command, args []string) error {
	inputs, err := resolveStdinArgs(args)
	if err != nil {
		return err
	}
	vars, err := substitute.ReadFile(buildArgs.envsubstFile)
	if err != nil {
		return err
	}
	substituter, err := substitute.New(vars, buildArgs.envsubstStrict)
	if err != nil {
		return err
	}
	reader, err := source.New(source.Options{SkipFiles: buildArgs.skipFiles, Stdin: stdinReader})
	if err != nil {
		return err
	}
	failed := false
	err = reader.Walk(cmd.Context(), inputs, func(e source.Event) {
		if e.Final {
			return
		}
		if e.Err != nil {
			cmd.PrintErrf("✗ %s: %s: %s\n", e.Source, validator.Reason(e.Reason), e.Err)
			failed = true
			return
		}
		raw := buildDocumentBody(e.Raw)
		if source.IsContentFree(raw) {
			return
		}
		rendered, subErr := substituter.Bytes(raw)
		if subErr != nil {
			messages := []string{subErr.Error()}
			if subErr.Reason == substitute.ReasonYAMLParseError {
				messages = substitute.YAMLErrors(subErr.Err)
			}
			for _, msg := range messages {
				cmd.PrintErrf("✗ %s - #%d: %s: %s\n", e.Source, e.DocIndex, validator.Reason(subErr.Reason), msg)
			}
			failed = true
			return
		}
		cmd.Printf("---\n# Source: %s\n", headerValue(e.Source))
		if e.Origin != "" {
			cmd.Printf("# Origin: %s\n", headerValue(e.Origin))
		}
		cmd.Printf("%s", rendered)
		if !bytes.HasSuffix(rendered, []byte("\n")) {
			cmd.Println()
		}
	})
	if err != nil {
		return err
	}
	if failed {
		return errSilent
	}
	return nil
}

// Replace an existing leading document marker with our provenance header,
// retaining any comment on the marker line.
func buildDocumentBody(raw []byte) []byte {
	// A stream BOM cannot appear between the provenance header and body.
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	line, rest, _ := bytes.Cut(raw, []byte("\n"))
	if after, ok := strings.CutPrefix(string(line), "---"); ok {
		if after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t") || after == "\r" {
			if comment := strings.TrimSpace(after); strings.HasPrefix(comment, "#") {
				return append([]byte(comment+"\n"), rest...)
			}
			if strings.TrimSpace(after) == "" {
				return rest
			}
		}
	}
	return raw
}

// headerValue quotes a path that would break out of its comment line.
func headerValue(s string) string {
	if strings.ContainsAny(s, "\r\n") {
		return strconv.Quote(s)
	}
	return s
}
