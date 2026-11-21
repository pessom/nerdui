// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/opencontainers/image-spec/identity"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// HistoryEntry is a step of an image's history (#47), newest first like `nerdctl image
// history`. Keep in sync with api/openapi.yaml.
type HistoryEntry struct {
	Created   *time.Time `json:"created,omitempty"`
	CreatedBy string     `json:"createdBy"`
	Comment   string     `json:"comment,omitempty"`
	// EmptyLayer steps (ENV, CMD, …) add no layer.
	EmptyLayer bool `json:"emptyLayer"`
	// Size of the layer: unpacked on disk when the image is unpacked (Compressed false),
	// otherwise as stored in the registry.
	Size       int64 `json:"size"`
	Compressed bool  `json:"compressed,omitempty"`
}

// History returns the history of the image named name for the host's platform.
func (ci clientImages) History(ctx context.Context, namespace, name string) ([]HistoryEntry, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: image reference is required", errdefs.ErrInvalidArgument)
	}
	cl, err := ci.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	rec, err := cl.ImageService().Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", name, classify(err))
	}
	img := containerd.NewImage(cl, rec)
	cfg, err := img.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("config of %s: %w", name, classify(err))
	}
	diffIDs, err := img.RootFS(ctx)
	if err != nil {
		return nil, fmt.Errorf("layers of %s: %w", name, classify(err))
	}
	var layers []ocispec.Descriptor
	if m, err := images.Manifest(ctx, cl.ContentStore(), rec.Target, platforms.Default()); err == nil {
		layers = m.Layers
	}
	snapshots := cl.SnapshotService(ci.c.nerdctlFor(namespace).Snapshotter)
	out := make([]HistoryEntry, 0, len(cfg.History))
	layer := 0
	for _, h := range cfg.History {
		e := HistoryEntry{CreatedBy: h.CreatedBy, Comment: h.Comment, EmptyLayer: h.EmptyLayer}
		if h.Created != nil && !h.Created.IsZero() {
			t := h.Created.UTC()
			e.Created = &t
		}
		if !h.EmptyLayer {
			if layer < len(diffIDs) {
				chain := identity.ChainID(diffIDs[:layer+1]).String()
				if u, err := snapshots.Usage(ctx, chain); err == nil {
					e.Size = u.Size
				} else if layer < len(layers) {
					e.Size, e.Compressed = layers[layer].Size, true
				}
			}
			layer++
		}
		out = append(out, e)
	}
	// Newest first, as nerdctl and docker show it.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
