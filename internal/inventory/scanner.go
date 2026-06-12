// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
	"github.com/fluxcd/flux-schema/internal/yamldoc"
)

const (
	extYAML = ".yaml"
	extYML  = ".yml"

	chartFile = "Chart.yaml"
	extTF     = ".tf"
)

// DefaultSkipFiles is applied when Options.SkipFiles is nil. It hides
// dotfiles and dot-directories, matching validator.DefaultSkipFiles.
var DefaultSkipFiles = []string{".*"}

// Options configures a Scan call.
type Options struct {
	// SkipFiles holds glob patterns matched against file and directory
	// basenames; matches are excluded from the scan. Nil applies
	// DefaultSkipFiles.
	SkipFiles []string
}

// Resource is a Kubernetes resource found in the repository.
type Resource struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string

	// Source is the defining file path relative to the scanned root,
	// "/"-separated.
	Source string
}

// Result is the outcome of a Scan.
type Result struct {
	// Resources lists every Kubernetes resource in walk order, excluding
	// kustomize configuration documents and kustomize patch files.
	Resources []Resource

	// DirTypes maps root-relative directory paths to their non-default
	// classification (kustomize-overlay, helm-chart, terraform-module).
	// Directories absent from the map hold plain Kubernetes manifests.
	DirTypes map[string]apiv1.InventoryDirectoryType

	// Files is the number of YAML files read, excluding kustomize patch
	// files.
	Files int

	// Lines is the total number of lines in the read YAML files,
	// excluding kustomize patch files.
	Lines int
}

// docHeader is the subset of a Kubernetes document needed to identify it.
type docHeader struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

// kustomizeConfig is the subset of a kustomize configuration document
// needed to resolve file-based patch references.
type kustomizeConfig struct {
	Patches []struct {
		Path string `json:"path"`
	} `json:"patches"`
	PatchesStrategicMerge []string `json:"patchesStrategicMerge"`
	PatchesJSON6902       []struct {
		Path string `json:"path"`
	} `json:"patchesJson6902"`
}

// IsFluxResource reports whether apiVersion belongs to a Flux API group:
// the group segment contains "fluxcd", covering *.toolkit.fluxcd.io and
// fluxcd.controlplane.io. Core-group documents (no "/") never match.
func IsFluxResource(apiVersion string) bool {
	group, _, ok := strings.Cut(apiVersion, "/")
	if !ok {
		return false
	}
	return strings.Contains(group, "fluxcd")
}

// isKustomizeConfig reports whether apiVersion is a kustomize
// configuration document rather than a Kubernetes resource.
func isKustomizeConfig(apiVersion string) bool {
	group, _, ok := strings.Cut(apiVersion, "/")
	return ok && group == "kustomize.config.k8s.io"
}

type scanner struct {
	// root confines every read to the scanned directory: opens are
	// resolved by the OS relative to it and cannot escape, even through
	// symbolic links. All paths held by the scanner are root-relative
	// and "/"-separated.
	root      *os.Root
	skipFiles []string

	dirTypes    map[string]apiv1.InventoryDirectoryType
	patchRefs   map[string]struct{}
	byFile      map[string][]Resource
	linesByFile map[string]int
	fileOrder   []string
}

// Scan inventories the YAML manifests under p, which may be a directory
// or a single file. For a file, the scanned root is its parent directory.
// Source and directory paths in the Result are relative to that root.
// Symbolic links encountered during the walk are never followed, and no
// read can escape the scanned root.
func Scan(p string, opts Options) (*Result, error) {
	skipFiles := opts.SkipFiles
	if skipFiles == nil {
		// Clone so DefaultSkipFiles can never be mutated through a
		// scanner's skipFiles slice.
		skipFiles = slices.Clone(DefaultSkipFiles)
	}
	for _, pat := range skipFiles {
		if strings.TrimSpace(pat) == "" {
			return nil, errors.New("skip file pattern must not be empty")
		}
		if _, err := filepath.Match(pat, "probe"); err != nil {
			return nil, fmt.Errorf("skip file pattern %q: %w", pat, err)
		}
	}

	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}

	s := &scanner{
		skipFiles:   skipFiles,
		dirTypes:    map[string]apiv1.InventoryDirectoryType{},
		patchRefs:   map[string]struct{}{},
		byFile:      map[string][]Resource{},
		linesByFile: map[string]int{},
	}

	rootDir := p
	if !info.IsDir() {
		rootDir = filepath.Dir(p)
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	s.root = root

	if !info.IsDir() {
		if err := s.scanFile(filepath.Base(p)); err != nil {
			return nil, err
		}
		return s.result(), nil
	}

	err = fs.WalkDir(root.FS(), ".", func(entry string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip symbolic links. os.Root already guarantees an escaping
		// link cannot be read, so this is not a containment measure: a
		// link whose target is inside the root would otherwise list the
		// same resource twice, since the walk visits the real target
		// directly. WalkDir does not descend symlinked directories in
		// the first place.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if entry != "." && s.matchSkipFile(d.Name()) {
				return fs.SkipDir
			}
			dirType, found, err := s.probeDirType(entry)
			if err != nil {
				return err
			}
			if found {
				s.dirTypes[entry] = dirType
				return fs.SkipDir
			}
			return nil
		}
		if s.matchSkipFile(d.Name()) {
			return nil
		}
		if ext := strings.ToLower(path.Ext(entry)); ext != extYAML && ext != extYML {
			return nil
		}
		return s.scanFile(entry)
	})
	if err != nil {
		return nil, err
	}
	return s.result(), nil
}

// matchSkipFile reports whether name (a directory or file basename)
// matches any skip-file glob pattern. Mirrors validator.matchSkipFile;
// patterns are validated up-front in Scan so the match call here cannot
// return an error.
func (s *scanner) matchSkipFile(name string) bool {
	for _, p := range s.skipFiles {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// probeDirType classifies dir as a Helm chart or Terraform module by its
// direct children. Chart.yaml wins when both markers are present.
func (s *scanner) probeDirType(dir string) (apiv1.InventoryDirectoryType, bool, error) {
	entries, err := fs.ReadDir(s.root.FS(), dir)
	if err != nil {
		return "", false, err
	}
	terraform := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == chartFile {
			return apiv1.InventoryDirectoryHelmChart, true, nil
		}
		if strings.HasSuffix(e.Name(), extTF) {
			terraform = true
		}
	}
	if terraform {
		return apiv1.InventoryDirectoryTerraformModule, true, nil
	}
	return "", false, nil
}

// scanFile splits the root-relative file rel into YAML documents and
// records every identifiable Kubernetes resource. Kustomize configuration
// documents mark the directory as an overlay and contribute their patch
// file references instead.
func (s *scanner) scanFile(rel string) error {
	f, err := s.root.Open(rel)
	if err != nil {
		return err
	}
	defer f.Close()

	s.fileOrder = append(s.fileOrder, rel)
	dir := path.Dir(rel)

	lc := &lineCounter{r: f}
	sc := yamldoc.NewScanner(lc)
	for sc.Scan() {
		raw := sc.Bytes()
		if isContentFree(raw) {
			continue
		}
		var hdr docHeader
		if err := yaml.Unmarshal(raw, &hdr); err != nil {
			continue
		}
		if hdr.APIVersion == "" || hdr.Kind == "" {
			continue
		}
		if isKustomizeConfig(hdr.APIVersion) {
			if _, ok := s.dirTypes[dir]; !ok {
				s.dirTypes[dir] = apiv1.InventoryDirectoryKustomizeOverlay
			}
			s.addPatchRefs(dir, raw)
			continue
		}
		s.byFile[rel] = append(s.byFile[rel], Resource{
			APIVersion: hdr.APIVersion,
			Kind:       hdr.Kind,
			Name:       hdr.Metadata.Name,
			Namespace:  hdr.Metadata.Namespace,
			Source:     rel,
		})
	}
	s.linesByFile[rel] = lc.count()
	return sc.Err()
}

// lineCounter counts lines as the wrapped reader is consumed: one per
// newline, plus one for a trailing line without a final newline.
type lineCounter struct {
	r        io.Reader
	newlines int
	lastByte byte
	read     bool
}

func (lc *lineCounter) Read(p []byte) (int, error) {
	n, err := lc.r.Read(p)
	if n > 0 {
		lc.read = true
		lc.newlines += bytes.Count(p[:n], []byte{'\n'})
		lc.lastByte = p[n-1]
	}
	return n, err
}

func (lc *lineCounter) count() int {
	if lc.read && lc.lastByte != '\n' {
		return lc.newlines + 1
	}
	return lc.newlines
}

// addPatchRefs collects the file-based patch references of a kustomize
// configuration document, resolved against the document's root-relative
// directory. Inline patches (multi-line strategic-merge entries) and
// references escaping the scanned root are ignored.
func (s *scanner) addPatchRefs(dir string, raw []byte) {
	var kc kustomizeConfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return
	}
	var refs []string
	for _, p := range kc.Patches {
		if p.Path != "" {
			refs = append(refs, p.Path)
		}
	}
	for _, p := range kc.PatchesStrategicMerge {
		if p != "" && !strings.Contains(p, "\n") {
			refs = append(refs, p)
		}
	}
	for _, p := range kc.PatchesJSON6902 {
		if p.Path != "" {
			refs = append(refs, p.Path)
		}
	}
	for _, ref := range refs {
		rel := path.Clean(path.Join(dir, filepath.ToSlash(ref)))
		if rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		s.patchRefs[rel] = struct{}{}
	}
}

// result assembles the Result, dropping files referenced as kustomize
// patches. Filtering after the walk avoids ordering dependencies: a
// kustomization may reference patch files in directories the walk has not
// visited yet.
func (s *scanner) result() *Result {
	res := &Result{
		Resources: []Resource{},
		DirTypes:  s.dirTypes,
	}
	for _, f := range s.fileOrder {
		if _, excluded := s.patchRefs[f]; excluded {
			continue
		}
		res.Files++
		res.Lines += s.linesByFile[f]
		res.Resources = append(res.Resources, s.byFile[f]...)
	}
	return res
}

// isContentFree reports whether raw is empty or contains only YAML
// comment lines. Mirrors validator.isContentFree.
func isContentFree(raw []byte) bool {
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
