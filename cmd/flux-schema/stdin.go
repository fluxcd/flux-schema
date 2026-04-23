// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"io"
	"os"

	"github.com/fluxcd/flux-schema/internal/validator"
)

const stdinLabel = validator.StdinSource

// stdinReader is the single source of truth for the stdin stream across
// every subcommand: readSource buffers from it for extract commands, and
// validate passes it as Options.Stdin so the validator streams from the
// same reader. A package variable so tests swap one thing rather than
// swapping os.Stdin and chasing every read site.
var stdinReader io.Reader = os.Stdin

// isStdinSentinel reports whether arg refers to the process's standard input.
// "-" is the cross-platform convention; "/dev/stdin" is kept as a Unix
// back-compat alias so existing scripts don't break.
func isStdinSentinel(arg string) bool {
	return arg == "-" || arg == "/dev/stdin"
}

// stdinIsPiped reports whether os.Stdin has data attached (pipe or redirect)
// rather than a terminal. Exposed as a package variable so tests can force
// a deterministic result regardless of how `go test` was invoked.
var stdinIsPiped = func() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// resolveStdinArgs normalizes positional arguments so every caller treats
// stdin identically. Four cases:
//
//   - args is empty AND stdin is piped → [stdinLabel]. Users can run
//     "cmd | flux-schema validate" without any sentinel.
//   - args is empty AND stdin is a terminal → error. Without this guard the
//     command would block on an empty read.
//   - args contains more than one stdin sentinel → error. Stdin is a
//     single non-rewindable stream and cannot serve as two sources.
//   - otherwise → each arg is returned as-is except a single stdin sentinel
//     ("-", "/dev/stdin") which is replaced with stdinLabel so downstream
//     code sees a single canonical source name.
func resolveStdinArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		if !stdinIsPiped() {
			return nil, errors.New("no input: pass a path or pipe data to stdin")
		}
		return []string{stdinLabel}, nil
	}
	out := make([]string, len(args))
	stdinCount := 0
	for i, a := range args {
		if isStdinSentinel(a) {
			stdinCount++
			out[i] = stdinLabel
		} else {
			out[i] = a
		}
	}
	if stdinCount > 1 {
		return nil, errors.New("stdin may only be referenced once per command")
	}
	return out, nil
}

// readSource reads the full contents of path, or the stdinReader if path
// is the stdin label. Used by commands that buffer input fully (extract
// crd/k8s) rather than streaming it through the validator.
func readSource(path string) ([]byte, error) {
	if path == stdinLabel {
		return io.ReadAll(stdinReader)
	}
	return os.ReadFile(path)
}
