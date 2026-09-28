// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package kustomize

import (
	"fmt"
	"path/filepath"
	"sync"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Doc is a rendered resource with its optional file origin.
type Doc struct {
	Raw    []byte
	Origin string
}

// Build renders dir with unrestricted file loading and disabled plugins,
// the equivalent of 'kustomize build --load-restrictor=LoadRestrictionsNone'.
// Origin tracking is enabled in memory to fill Doc.Origin; the origin
// annotation is removed from the rendered resources unless the root
// kustomization requested it through buildMetadata.
func Build(dir string) ([]Doc, error) {
	fs := filesys.MakeFsOnDisk()
	root, err := filesys.ConfirmDir(fs, dir)
	if err != nil {
		return nil, err
	}
	file, ok := Detect(root.String())
	if !ok {
		return nil, fmt.Errorf("no kustomization file found in %s", dir)
	}
	ofs := &originFS{FileSystem: fs, rootFile: file}
	res, err := build(ofs, dir)
	if err != nil {
		return nil, err
	}
	docs := make([]Doc, 0, res.Size())
	for _, r := range res.Resources() {
		origin, err := r.GetOrigin()
		if err != nil {
			return nil, fmt.Errorf("read origin for %s: %w", r.CurId(), err)
		}
		if ofs.injected {
			if err := r.SetOrigin(nil); err != nil {
				return nil, fmt.Errorf("remove origin for %s: %w", r.CurId(), err)
			}
			if err := yaml.ClearEmptyAnnotations(&r.RNode); err != nil {
				return nil, fmt.Errorf("remove empty annotations for %s: %w", r.CurId(), err)
			}
		}
		raw, err := r.AsYAML()
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", r.CurId(), err)
		}
		docs = append(docs, Doc{Raw: raw, Origin: originPath(dir, origin)})
	}
	return docs, nil
}

// Serialize builds because kustomize shares global OpenAPI state and maps.
// See https://github.com/kubernetes-sigs/kustomize/issues/3659.
var buildMutex sync.Mutex

// Adapted from fluxcd/pkg/kustomize.Build without its client-go dependencies.
func build(fs filesys.FileSystem, dir string) (res resmap.ResMap, err error) {
	buildMutex.Lock()
	defer buildMutex.Unlock()

	defer func() {
		if r := recover(); r != nil {
			res = nil
			err = fmt.Errorf("recovered from kustomize build panic: %v", r)
		}
	}()

	options := &krusty.Options{
		LoadRestrictions: types.LoadRestrictionsNone,
		PluginConfig:     types.DisabledPluginConfig(),
	}

	openapi.ResetOpenAPI()
	defer openapi.ResetOpenAPI()
	_ = openapi.Schema()

	return krusty.MakeKustomizer(options).Run(fs, dir)
}

// originFS adds originAnnotations to the buildMetadata of the root
// kustomization file as it is read, leaving the file on disk untouched.
// krusty strips origin annotations from the output unless the root
// kustomization requests them, so injecting the option is what makes
// the per-resource origin available.
type originFS struct {
	filesys.FileSystem
	rootFile string

	// injected is true when the root kustomization did not request
	// originAnnotations itself, so Build must strip them from the output.
	injected bool
}

func (fs *originFS) ReadFile(path string) ([]byte, error) {
	raw, err := fs.FileSystem.ReadFile(path)
	if err != nil || filepath.Clean(path) != fs.rootFile {
		return raw, err
	}
	fs.injected = false
	// On any parse problem, return the original bytes so kustomize
	// reports the error in its own terms.
	node, err := yaml.Parse(string(raw))
	if err != nil {
		return raw, nil
	}
	metadata, err := node.Pipe(yaml.LookupCreate(yaml.SequenceNode, "buildMetadata"))
	if err != nil {
		return raw, nil
	}
	elements, err := metadata.Elements()
	if err != nil {
		return raw, nil
	}
	for _, elem := range elements {
		if elem.YNode().Value == types.OriginAnnotations {
			return raw, nil
		}
	}
	if err := metadata.PipeE(yaml.Append(yaml.NewStringRNode(types.OriginAnnotations).YNode())); err != nil {
		return raw, nil
	}
	rendered, err := node.String()
	if err != nil {
		return raw, nil
	}
	fs.injected = true
	return []byte(rendered), nil
}

func originPath(dir string, origin *resource.Origin) string {
	if origin == nil || origin.Path == "" {
		return ""
	}
	if origin.Repo != "" {
		path := origin.Repo + "//" + origin.Path
		if origin.Ref != "" {
			path += "?ref=" + origin.Ref
		}
		return path
	}
	if filepath.IsAbs(origin.Path) {
		return filepath.Clean(origin.Path)
	}
	return filepath.Join(dir, origin.Path)
}
