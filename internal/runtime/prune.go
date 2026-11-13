// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"
	"slices"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/buildkitutil"
	nbuilder "github.com/containerd/nerdctl/v2/pkg/cmd/builder"
	ncontainer "github.com/containerd/nerdctl/v2/pkg/cmd/container"
	nimage "github.com/containerd/nerdctl/v2/pkg/cmd/image"
	nnetwork "github.com/containerd/nerdctl/v2/pkg/cmd/network"
	"github.com/containerd/nerdctl/v2/pkg/imgutil"
	"github.com/docker/go-units"
)

// PruneOptions selects what a prune removes (#52, ADR-0020). Volumes are not pruned by nerdui.
type PruneOptions struct {
	// Containers removes stopped containers.
	Containers bool
	// Images removes dangling images, or all images unused by containers with AllImages.
	Images    bool
	AllImages bool
	// Networks removes networks no container uses (except the default ones).
	Networks bool
	// BuildCache removes the BuildKit cache (all of it with AllImages, like nerdctl).
	BuildCache bool
}

// PrunePreview estimates what a prune removes. Images are exact (nerdctl's own selection);
// containers are those stopped now; networks and build cache are not previewed.
type PrunePreview struct {
	Containers []PrunedItem `json:"containers"`
	Images     []PrunedItem `json:"images"`
	// ImagesSize sums the unpacked sizes of the images; shared layers make the freed space
	// smaller.
	ImagesSize int64 `json:"imagesSize"`
	// BuildKit reports whether a BuildKit daemon is available for build cache pruning.
	BuildKit bool `json:"buildKit"`
}

// PrunedItem is a container or image a prune would remove.
type PrunedItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
}

// networkDriversToKeep are never pruned, as in `nerdctl network prune`.
var networkDriversToKeep = []string{"host", "none", "bridge"}

// PrunePreview lists what a prune of the namespace would remove.
func (c *Client) PrunePreview(ctx context.Context, namespace string, allImages bool) (PrunePreview, error) {
	cl, err := c.conn()
	if err != nil {
		return PrunePreview{}, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	out := PrunePreview{Containers: []PrunedItem{}, Images: []PrunedItem{}}

	list, err := c.Containers().List(ctx, namespace)
	if err != nil {
		return PrunePreview{}, err
	}
	for _, ct := range list {
		if ct.State == StateExited || ct.State == StateCreated {
			out.Containers = append(out.Containers, PrunedItem{ID: ct.ID, Name: ct.Name})
		}
	}

	candidates, err := pruneCandidates(ctx, cl, allImages)
	if err != nil {
		return PrunePreview{}, fmt.Errorf("select images: %w", classify(err))
	}
	if len(candidates) > 0 {
		listed, err := c.Images().List(ctx, namespace)
		if err != nil {
			return PrunePreview{}, err
		}
		for _, img := range candidates {
			item := PrunedItem{ID: img.Target.Digest.String(), Name: img.Name}
			if i := slices.IndexFunc(listed, func(l Image) bool { return l.Name == img.Name }); i >= 0 {
				item.Size = listed[i].Size
			}
			out.ImagesSize += item.Size
			out.Images = append(out.Images, item)
		}
	}
	if _, err := buildkitutil.GetBuildkitHost(namespace); err == nil {
		out.BuildKit = true
	}
	return out, nil
}

func pruneCandidates(ctx context.Context, cl *containerd.Client, all bool) ([]images.Image, error) {
	if all {
		return imgutil.GetUnusedImages(ctx, cl)
	}
	return imgutil.GetDanglingImages(ctx, cl)
}

// Prune runs nerdctl's prunes in the order of `nerdctl system prune` and writes their output
// to out. A missing BuildKit daemon skips the build cache with a note instead of failing.
func (c *Client) Prune(ctx context.Context, namespace string, opts PruneOptions, out io.Writer) error {
	cl, err := c.conn()
	if err != nil {
		return err
	}
	ctx = withJobLogger(namespaces.WithNamespace(ctx, namespace), out)
	g := types.GlobalCommandOptions(*c.nerdctlFor(namespace))
	before := c.imageUsage(ctx, namespace)
	section := func(title string) { _, _ = fmt.Fprintf(out, "\x1b[1m%s\x1b[0m\r\n", title) }

	if opts.Containers {
		section("Stopped containers")
		if err := ncontainer.Prune(ctx, cl, types.ContainerPruneOptions{GOptions: g, Stdout: out}); err != nil {
			return fmt.Errorf("prune containers: %w", classify(err))
		}
	}
	if opts.Networks {
		section("Unused networks")
		if err := nnetwork.Prune(ctx, cl, types.NetworkPruneOptions{
			GOptions: g, NetworkDriversToKeep: networkDriversToKeep, Stdout: out,
		}); err != nil {
			return fmt.Errorf("prune networks: %w", classify(err))
		}
	}
	if opts.Images {
		section("Images")
		if err := nimage.Prune(ctx, cl, types.ImagePruneOptions{GOptions: g, All: opts.AllImages, Stdout: out}); err != nil {
			return fmt.Errorf("prune images: %w", classify(err))
		}
	}
	if opts.BuildCache {
		section("Build cache")
		host, err := buildkitutil.GetBuildkitHost(namespace)
		if err != nil {
			_, _ = fmt.Fprintf(out, "skipped: no BuildKit daemon (%v)\r\n", err)
		} else {
			pruned, err := nbuilder.Prune(ctx, types.BuilderPruneOptions{
				Stderr: out, GOptions: g, All: opts.AllImages, BuildKitHost: host,
			})
			if err != nil {
				return fmt.Errorf("prune build cache: %w", err)
			}
			_, _ = fmt.Fprintf(out, "Deleted %d build cache objects\r\n", len(pruned))
		}
	}
	if after := c.imageUsage(ctx, namespace); before >= 0 && after >= 0 && before > after {
		_, _ = fmt.Fprintf(out, "\r\nImage disk usage reduced by about %s\r\n", units.HumanSize(float64(before-after)))
	}
	return nil
}

// imageUsage sums unpacked and content sizes of the namespace's images; -1 when unknown.
func (c *Client) imageUsage(ctx context.Context, namespace string) int64 {
	list, err := c.Images().List(ctx, namespace)
	if err != nil {
		return -1
	}
	var total int64
	for _, i := range list {
		total += i.Size + i.ContentSize
	}
	return total
}
