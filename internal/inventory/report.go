// Copyright 2026 The Flux Authors
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"maps"
	"path"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/fluxcd/flux-schema/api/v1beta1"
)

// NewInventory assembles an Inventory envelope from a Scan result. The
// caller owns the reporter string (typically "flux-schema/"+VERSION) and
// the timestamp so tests can pin both. Output is deterministic: map keys
// serialize sorted, and identity lists keep the document order within
// their file.
func NewInventory(reporter string, timestamp time.Time, res *Result) apiv1.Inventory {
	body := apiv1.InventorySpec{
		Reporter:    reporter,
		Timestamp:   timestamp.UTC().Format(time.RFC3339),
		Directories: map[string]apiv1.InventoryDirectoryType{},
		Resources:   map[string]int{},
		Flux:        map[string]map[string][]string{},
	}

	for _, r := range res.Resources {
		dir := path.Dir(r.Source)
		if _, ok := body.Directories[dir]; !ok {
			body.Directories[dir] = apiv1.InventoryDirectoryKubernetesManifests
		}

		body.Resources[r.APIVersion+"/"+r.Kind]++
		if IsFluxResource(r.APIVersion) {
			if body.Flux[r.Kind] == nil {
				body.Flux[r.Kind] = map[string][]string{}
			}
			body.Flux[r.Kind][r.Source] = append(body.Flux[r.Kind][r.Source], identity(r))
		}
	}

	// Typed directories (overlays, charts, terraform) override the
	// manifests default and add entries for pruned or resource-free dirs.
	maps.Copy(body.Directories, res.DirTypes)

	body.Summary.Files = res.Files
	body.Summary.Resources = len(res.Resources)
	body.Summary.LinesOfYAML = res.Lines

	return apiv1.Inventory{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiv1.GroupVersion.String(),
			Kind:       apiv1.InventoryKind,
		},
		Schema:    apiv1.InventorySchema,
		Inventory: body,
	}
}

// identity encodes a resource as "namespace/name". The namespace
// segment is dropped when empty and the name falls back to "-" when the
// document has no metadata.name.
func identity(r Resource) string {
	name := r.Name
	if name == "" {
		name = "-"
	}
	if r.Namespace != "" {
		name = r.Namespace + "/" + name
	}
	return name
}
