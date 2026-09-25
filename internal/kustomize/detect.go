// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package kustomize

import (
	"os"
	"path/filepath"

	"sigs.k8s.io/kustomize/api/konfig"
)

// Detect finds a recognized kustomization file without parsing it.
func Detect(dir string) (file string, ok bool) {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}
