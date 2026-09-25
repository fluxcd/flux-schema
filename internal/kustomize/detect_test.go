// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package kustomize

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/api/konfig"
)

func TestDetect(t *testing.T) {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			g.Expect(os.WriteFile(path, []byte("not valid yaml: ["), 0o644)).To(Succeed())
			file, ok := Detect(dir)
			g.Expect(ok).To(BeTrue())
			g.Expect(file).To(Equal(path))
		})
	}
	for _, name := range []string{"missing", "directory", "unrecognized"} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			switch name {
			case "directory":
				g.Expect(os.Mkdir(filepath.Join(dir, "kustomization.yaml"), 0o755)).To(Succeed())
			case "unrecognized":
				g.Expect(os.WriteFile(filepath.Join(dir, "custom.yaml"), nil, 0o644)).To(Succeed())
			}
			file, ok := Detect(dir)
			g.Expect(ok).To(BeFalse())
			g.Expect(file).To(BeEmpty())
		})
	}
}
