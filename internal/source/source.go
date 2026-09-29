// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

// Package source streams YAML files, stdin, and kustomize builds in discovery order.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sigs.k8s.io/kustomize/api/konfig"

	"github.com/fluxcd/flux-schema/internal/kustomize"
	"github.com/fluxcd/flux-schema/internal/yamldoc"
)

const (
	StdinSource               = "stdin"
	ReasonSourceLoadError     = "source-load-error"
	ReasonKustomizeBuildError = "kustomize-build-error"
)

// DefaultSkipFiles hides dotfiles and dot-directories during walks.
var DefaultSkipFiles = []string{".*"}

// Event carries an owned document, a source-level error, or a Final sentinel.
// Final marks the end of production, not completion of downstream processing.
// SourceIndex is zero-based discovery order; DocIndex is one-based, or zero
// for source errors and sentinels.
type Event struct {
	Source      string
	SourceIndex int
	Origin      string
	DocIndex    int
	Raw         []byte
	Err         error
	Reason      string
	Final       bool
}

// Options configures input discovery. Nil SkipFiles uses DefaultSkipFiles.
// Stdin must be set when a path equals StdinSource.
type Options struct {
	SkipFiles []string
	Stdin     io.Reader
}

// Reader discovers sources with immutable skip patterns.
type Reader struct {
	skipFiles []string
	stdin     io.Reader
}

// New validates skip patterns before any sources are read.
func New(opts Options) (*Reader, error) {
	patterns := opts.SkipFiles
	if patterns == nil {
		patterns = DefaultSkipFiles
	}
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			return nil, errors.New("skip file pattern must not be empty")
		}
		if _, err := filepath.Match(p, "probe"); err != nil {
			return nil, fmt.Errorf("skip file pattern %q: %w", p, err)
		}
	}
	return &Reader{skipFiles: slices.Clone(patterns), stdin: opts.Stdin}, nil
}

func (r *Reader) matchSkipFile(name string) bool {
	for _, p := range r.skipFiles {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Walk emits events synchronously in source and document order. Read, build,
// and traversal errors are emitted for their source; remaining paths continue.
// Only context cancellation is returned as an error.
func (r *Reader) Walk(ctx context.Context, paths []string, emit func(Event)) error {
	s := &stream{ctx: ctx, emit: emit}
	for _, path := range paths {
		if err := r.produceFromPath(s, path); err != nil && ctx.Err() == nil {
			in := s.newSource(path)
			_ = in.send(Event{Err: err, Reason: ReasonSourceLoadError})
			in.finish()
		}
		if ctx.Err() != nil {
			break
		}
	}
	return ctx.Err()
}

type stream struct {
	ctx       context.Context
	emit      func(Event)
	nextIndex int
}

type input struct {
	stream *stream
	label  string
	index  int
}

func (s *stream) newSource(label string) *input {
	in := &input{stream: s, label: label, index: s.nextIndex}
	s.nextIndex++
	return in
}

func (in *input) send(e Event) error {
	if err := in.stream.ctx.Err(); err != nil {
		return err
	}
	e.Source = in.label
	e.SourceIndex = in.index
	in.stream.emit(e)
	return in.stream.ctx.Err()
}

func (in *input) finish() {
	_ = in.send(Event{Final: true})
}

func (r *Reader) produceFromPath(s *stream, path string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if path == StdinSource {
		if r.stdin == nil {
			return fmt.Errorf("source %q requires Options.Stdin to be set", StdinSource)
		}
		in := s.newSource(StdinSource)
		defer in.finish()
		return in.streamReader(r.stdin)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if err := s.ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if p != path && r.matchSkipFile(d.Name()) {
					return filepath.SkipDir
				}
				if file, ok := kustomize.Detect(p); ok && !r.matchSkipFile(filepath.Base(file)) {
					if err := s.streamBuild(p); err != nil {
						return err
					}
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if (ext != ".yaml" && ext != ".yml") || r.matchSkipFile(d.Name()) {
				return nil
			}
			in := s.newSource(p)
			defer in.finish()
			return in.streamFile(p)
		})
	}
	if base := filepath.Base(path); slices.Contains(konfig.RecognizedKustomizationFileNames(), base) && !r.matchSkipFile(base) {
		return s.streamBuild(filepath.Dir(path))
	}
	in := s.newSource(path)
	defer in.finish()
	return in.streamFile(path)
}

func (s *stream) streamBuild(dir string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	dir = filepath.Clean(dir)
	in := s.newSource(dir)
	defer in.finish()
	docs, err := kustomize.Build(dir)
	if err != nil {
		return in.send(Event{Err: err, Reason: ReasonKustomizeBuildError})
	}
	for i, doc := range docs {
		if err := in.send(Event{Origin: doc.Origin, DocIndex: i + 1, Raw: doc.Raw}); err != nil {
			return err
		}
	}
	return nil
}

func (in *input) streamFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return in.send(Event{Err: err, Reason: ReasonSourceLoadError})
	}
	defer f.Close()
	return in.streamReader(f)
}

func (in *input) streamReader(r io.Reader) error {
	scanner := yamldoc.NewScanner(r)
	idx := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if IsContentFree(raw) {
			continue
		}
		idx++
		if err := in.send(Event{DocIndex: idx, Raw: bytes.Clone(raw)}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return in.send(Event{
			Err: fmt.Errorf("scan %s: %w", in.label, err), Reason: ReasonSourceLoadError,
		})
	}
	return nil
}

// IsContentFree reports whether raw is empty or contains only YAML comment
// lines. Dropping these before assigning DocIndex keeps numbering aligned
// with real documents.
func IsContentFree(raw []byte) bool {
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if trimmed[0] != '#' {
			return false
		}
	}
	return true
}
