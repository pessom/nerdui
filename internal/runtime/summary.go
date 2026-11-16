// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"path/filepath"
	"slices"
	"sort"

	"github.com/containerd/containerd/v2/pkg/namespaces"
)

// ContainerCounts counts containers by state (#63).
type ContainerCounts struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Paused    int `json:"paused"`
	Stopped   int `json:"stopped"`
	Unhealthy int `json:"unhealthy"`
}

func (c *ContainerCounts) add(o ContainerCounts) {
	c.Total += o.Total
	c.Running += o.Running
	c.Paused += o.Paused
	c.Stopped += o.Stopped
	c.Unhealthy += o.Unhealthy
}

// NamespaceSummary counts the resources of one namespace.
type NamespaceSummary struct {
	Name       string          `json:"name"`
	Containers ContainerCounts `json:"containers"`
	Images     int             `json:"images"`
	Volumes    int             `json:"volumes"`
	Networks   int             `json:"networks"`
}

// Filesystem is a filesystem holding container data.
type Filesystem struct {
	// Paths are the data directories on it (containerd's root, nerdctl's data root).
	Paths []string `json:"paths"`
	Total uint64   `json:"total"`
	Free  uint64   `json:"free"`
}

// Summary is the overview of a host across namespaces (#63). Keep in sync with
// api/openapi.yaml.
type Summary struct {
	Containers ContainerCounts    `json:"containers"`
	Images     int                `json:"images"`
	Volumes    int                `json:"volumes"`
	Networks   int                `json:"networks"`
	Namespaces []NamespaceSummary `json:"namespaces"`
	// ImagesSize is the unpacked and content size of the images (shared layers counted once
	// per image).
	ImagesSize int64 `json:"imagesSize"`
	// VolumesSize is only computed on request: it walks every volume.
	VolumesSize *int64       `json:"volumesSize,omitempty"`
	Filesystems []Filesystem `json:"filesystems"`
}

// Summary counts containers, images, volumes and networks of every namespace and reports the
// disk usage of images (and volumes with volumeSizes) and the data filesystems.
func (c *Client) Summary(ctx context.Context, volumeSizes bool) (Summary, error) {
	nss, err := c.Namespaces(ctx)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Namespaces: []NamespaceSummary{}, Filesystems: []Filesystem{}}
	var volSize int64
	for _, ns := range nss {
		n := NamespaceSummary{Name: ns}
		containers, err := c.Containers().List(ctx, ns)
		if err != nil {
			return Summary{}, err
		}
		for _, ct := range containers {
			n.Containers.Total++
			switch ct.State {
			case StateRunning, StateRestarting:
				n.Containers.Running++
			case StatePaused:
				n.Containers.Paused++
			default:
				n.Containers.Stopped++
			}
			if ct.Health == "unhealthy" {
				n.Containers.Unhealthy++
			}
		}
		images, err := c.Images().List(ctx, ns)
		if err != nil {
			return Summary{}, err
		}
		n.Images = len(images)
		seen := map[string]bool{}
		for _, img := range images {
			if !seen[img.Digest] {
				seen[img.Digest] = true
				out.ImagesSize += img.Size + img.ContentSize
			}
		}
		vols, err := c.Volumes().List(ctx, ns, volumeSizes)
		if err != nil {
			return Summary{}, err
		}
		n.Volumes = len(vols)
		for _, v := range vols {
			if v.Size != nil {
				volSize += *v.Size
			}
		}
		if nets, err := c.Networks().List(ctx, ns); err == nil {
			n.Networks = len(nets)
		}
		out.Containers.add(n.Containers)
		out.Images += n.Images
		out.Volumes += n.Volumes
		out.Networks += n.Networks
		out.Namespaces = append(out.Namespaces, n)
	}
	if volumeSizes {
		out.VolumesSize = &volSize
	}
	out.Filesystems = c.dataFilesystems(ctx)
	return out, nil
}

// dataFilesystems reports the filesystems of containerd's content store and nerdctl's data
// root, once per filesystem.
func (c *Client) dataFilesystems(ctx context.Context) []Filesystem {
	var paths []string
	if cl, err := c.conn(); err == nil {
		filters := []string{`type=="io.containerd.content.v1"`, `type=="io.containerd.snapshotter.v1"`}
		if plugins, err := cl.IntrospectionService().Plugins(namespaces.WithNamespace(ctx, c.namespace), filters...); err == nil {
			for _, p := range plugins.Plugins {
				// Plugin roots are "<containerd root>/<plugin id>": report the containerd root.
				if root := p.Exports["root"]; root != "" && p.InitErr == nil && !slices.Contains(paths, filepath.Dir(root)) {
					paths = append(paths, filepath.Dir(root))
				}
			}
		}
	}
	if root := c.nerdctlConfig.DataRoot; root != "" {
		paths = append(paths, root)
	}
	byFS := map[string]*Filesystem{}
	var order []string
	for _, p := range paths {
		id, total, free, err := statFS(p)
		if err != nil {
			continue
		}
		fs, ok := byFS[id]
		if !ok {
			fs = &Filesystem{Total: total, Free: free}
			byFS[id] = fs
			order = append(order, id)
		}
		fs.Paths = append(fs.Paths, p)
	}
	out := make([]Filesystem, 0, len(order))
	for _, id := range order {
		sort.Strings(byFS[id].Paths)
		out = append(out, *byFS[id])
	}
	return out
}
