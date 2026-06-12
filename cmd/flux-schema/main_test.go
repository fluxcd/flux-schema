// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/fluxcd/flux-schema/internal/flags"
	"github.com/fluxcd/flux-schema/internal/validator"
)

var (
	timeout = 30 * time.Second
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

// executeCommand executes a command with the given args
// and returns the output and error. It resets the command
// arguments to their default values after execution to
// ensure test isolation.
func executeCommand(args []string) (string, error) {
	defer resetCmdArgs()

	buf := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetArgs(args)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.Execute()
	return buf.String(), err
}

func resetCmdArgs() {
	rootArgs.timeout = timeout

	versionArgs = versionFlags{output: "text"}
	extractCRDArgs = extractCRDFlags{ExtractOutput: flags.NewExtractOutput()}
	extractK8sArgs = extractK8sFlags{ExtractOutput: flags.NewExtractOutput()}
	extractOpenShiftArgs = extractOpenShiftFlags{ExtractOutput: flags.NewExtractOutput()}
	validateArgs = validateFlags{concurrent: validator.DefaultWorkers, output: "text"}
	discoverArgs = discoverFlags{output: "text"}

	// pflag.Flag.Changed persists across Execute calls on the shared rootCmd,
	// which breaks MarkFlagRequired validation in subsequent tests. Clear it
	// for every flag in the command tree.
	resetFlagChanged(rootCmd)
}

func resetFlagChanged(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	for _, sub := range cmd.Commands() {
		resetFlagChanged(sub)
	}
}

// replaceStdin points both os.Stdin and stdinReader at a pipe so the
// stdinIsPiped fd-mode check and every read site see the same stream.
// The writer goroutine is joined on cleanup so oversized payloads
// (bigger than the ~64 KiB kernel pipe buffer) fail instead of hanging.
func replaceStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdin := os.Stdin
	origReader := stdinReader
	os.Stdin = r
	stdinReader = r
	done := make(chan error, 1)
	go func() {
		_, werr := w.Write([]byte(data))
		_ = w.Close()
		done <- werr
	}()
	t.Cleanup(func() {
		os.Stdin = origStdin
		stdinReader = origReader
		_ = r.Close()
		// EPIPE/ErrClosedPipe are expected when a test returns before draining.
		if err := <-done; err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("stdin writer: %v", err)
		}
	})
}

// forceStdinTTY pins stdinIsPiped() to false so no-pipe assertions pass
// regardless of how `go test` was invoked.
func forceStdinTTY(t *testing.T) {
	t.Helper()
	orig := stdinIsPiped
	stdinIsPiped = func() bool { return false }
	t.Cleanup(func() { stdinIsPiped = orig })
}
