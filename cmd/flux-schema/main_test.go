// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"testing"
	"time"
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

// resetCmdArgs resets the command arguments
// to their default values.
func resetCmdArgs() {
	rootArgs.timeout = timeout

	versionArgs = versionFlags{output: "text"}
}
