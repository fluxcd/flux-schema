// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package yamldoc provides a bufio.SplitFunc that splits a byte stream
// into individual YAML documents on "\n---" separators.
//
// SplitDocument is a line-oriented splitter, not a YAML parser: a literal
// "\n---" inside a string will still terminate a document. This matches
// Kubernetes' unexported pkg/util/yaml.splitYAMLDocument behavior,
// used in kubectl.
package yamldoc

import (
	"bufio"
	"bytes"
	"io"
)

// Scanner buffer sizes — start small so walking a tree of thousands of
// small manifests doesn't pay a multi-megabyte allocation per file; cap
// at 256 MiB so pathological documents still parse.
const (
	initialBufferSize = 64 * 1024
	maxBufferSize     = 256 * 1024 * 1024
)

// NewScanner returns a bufio.Scanner pre-configured to read YAML documents
// from r: SplitDocument as the split function and a buffer that grows up to
// maxBufferSize.
func NewScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, initialBufferSize), maxBufferSize)
	s.Split(SplitDocument)
	return s
}

// Split reads every document from data and returns an owned copy of each.
// The returned slices do not alias data and are safe to retain after the
// caller has released the input buffer.
func Split(data []byte) [][]byte {
	s := NewScanner(bytes.NewReader(data))
	var out [][]byte
	for s.Scan() {
		tok := s.Bytes()
		buf := make([]byte, len(tok))
		copy(buf, tok)
		out = append(out, buf)
	}
	return out
}

// SplitDocument is a bufio.SplitFunc for YAML documents.
//
// Ported from Kubernetes' pkg/util/yaml.splitYAMLDocument
// (Apache-2.0, Copyright The Kubernetes Authors).
func SplitDocument(data []byte, atEOF bool) (advance int, token []byte, err error) {
	const yamlSeparator = "\n---"
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	sep := len([]byte(yamlSeparator))
	if i := bytes.Index(data, []byte(yamlSeparator)); i >= 0 {
		i += sep
		after := data[i:]
		if len(after) == 0 {
			if atEOF {
				return len(data), data[:len(data)-sep], nil
			}
			return 0, nil, nil
		}
		if j := bytes.IndexByte(after, '\n'); j >= 0 {
			return i + j + 1, data[0 : i-sep], nil
		}
		return 0, nil, nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
