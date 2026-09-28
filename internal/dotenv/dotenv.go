// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package dotenv reads files of NAME=value lines as literal variables.
// Values are taken exactly as written, the same way Flux uses
// postBuild.substitute values: quotes are kept, $ references are not
// expanded, and nothing is executed or read from the process environment.
package dotenv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// namePattern matches the variable names accepted by kustomize-controller
// post-build substitution.
var namePattern = regexp.MustCompile(`^[_[:alpha:]][_[:alpha:][:digit:]]*$`)

// Read parses the file at path. See Parse for the format.
func Read(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads NAME=value lines from r. Blank lines and lines starting with
// '#' are skipped, leading spaces and tabs are ignored, and the value is
// everything after the first '=' up to the line ending (LF or CRLF). Any
// other line is an error reported with its line number. A later definition
// of a name replaces an earlier one.
func Parse(r io.Reader) (map[string]string, error) {
	vars := map[string]string{}
	reader := bufio.NewReader(r)
	for n := 1; ; n++ {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if line == "" && err == io.EOF {
			return vars, nil
		}
		if strings.HasSuffix(line, "\n") {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		}
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		text := strings.TrimLeft(line, " \t")
		if text != "" && !strings.HasPrefix(text, "#") {
			name, value, ok := strings.Cut(text, "=")
			if !ok {
				return nil, fmt.Errorf("line %d: expected NAME=value", n)
			}
			if !namePattern.MatchString(name) {
				return nil, fmt.Errorf("line %d: invalid variable name %q, must match %q", n, name, namePattern)
			}
			vars[name] = value
		}
		if err == io.EOF {
			return vars, nil
		}
	}
}
