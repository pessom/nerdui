// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	nvolume "github.com/containerd/nerdctl/v2/pkg/cmd/volume"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
	"github.com/containerd/nerdctl/v2/pkg/inspecttypes/dockercompat"
	"github.com/containerd/nerdctl/v2/pkg/inspecttypes/native"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/mountutil"
	"github.com/containerd/nerdctl/v2/pkg/store"
)

// Volumes are nerdctl's named volumes (#57): directories in nerdctl's data root, per
// namespace, shared with the nerdctl CLI.
type Volumes interface {
	// List returns the volumes of a namespace sorted by name, with the containers that mount
	// them; size walks every volume and is only computed on request.
	List(ctx context.Context, namespace string, size bool) ([]Volume, error)
	// Create creates a volume like `nerdctl volume create`; an existing name is a conflict.
	Create(ctx context.Context, namespace, name string, lbls map[string]string) (Volume, error)
	// Remove removes a volume that no container (running or not) mounts; 409 otherwise.
	Remove(ctx context.Context, namespace, name string) error
	// ListFiles, PreviewFile and Download read the contents of a volume on the host (#58).
	ListFiles(namespace, name, dir string) ([]VolumeEntry, bool, error)
	PreviewFile(namespace, name, file string) (VolumeFile, error)
	Download(namespace, name, target string) (VolumeDownload, error)
}

// Volume is a named volume. Keep in sync with api/openapi.yaml.
type Volume struct {
	Name       string            `json:"name"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	// Anonymous volumes were created for a container without a name (`-v /data`).
	Anonymous bool `json:"anonymous"`
	// CreatedAt is the modification time of the volume directory (nerdctl keeps no date).
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// Size in bytes; only with size=true.
	Size       *int64          `json:"size,omitempty"`
	Containers []VolumeMounter `json:"containers"`
}

// VolumeMounter is a container that mounts a volume.
type VolumeMounter struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Volumes returns volume operations of the local containerd.
func (c *Client) Volumes() Volumes { return clientVolumes{c} }

type clientVolumes struct{ c *Client }

func (cv clientVolumes) List(ctx context.Context, namespace string, size bool) ([]Volume, error) {
	cl, err := cv.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	cfg := cv.c.nerdctlFor(namespace)
	vols, err := nvolume.Volumes(namespace, cfg.DataRoot, cfg.Address, size, nil)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	used, err := volumeMounters(ctx, cl)
	if err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(vols))
	for _, v := range vols {
		out = append(out, toVolume(v, size, used[v.Name]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toVolume(v native.Volume, size bool, mounters []VolumeMounter) Volume {
	out := Volume{Name: v.Name, Mountpoint: v.Mountpoint, Labels: map[string]string{}, Containers: mounters}
	if v.Labels != nil {
		for k, val := range *v.Labels {
			out.Labels[k] = val
		}
	}
	if _, ok := out.Labels[labels.AnonymousVolumes]; ok {
		out.Anonymous = true
		delete(out.Labels, labels.AnonymousVolumes)
	}
	if size {
		s := v.Size
		out.Size = &s
	}
	if out.Containers == nil {
		out.Containers = []VolumeMounter{}
	}
	// The volume directory is the parent of the mount point ("<dir>/_data").
	if fi, err := os.Stat(filepath.Dir(v.Mountpoint)); err == nil {
		out.CreatedAt = fi.ModTime().UTC()
	}
	return out
}

// volumeMounters returns, per volume name, the containers of the namespace (in ctx) that mount
// it, from the mounts nerdctl records in container labels (as `nerdctl volume rm` checks). Mounts
// are stored in "nerdctl/mounts" or, since nerdctl 2.3, split into "nerdctl/mounts.N" labels;
// labels.GetMount reads both. Unreadable mounts are an error: removal must fail closed.
func volumeMounters(ctx context.Context, cl *containerd.Client) (map[string][]VolumeMounter, error) {
	containers, err := cl.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", classify(err))
	}
	out := map[string][]VolumeMounter{}
	for _, c := range containers {
		lbls, err := c.Labels(ctx)
		if err != nil {
			continue // removed while listing
		}
		names, err := mountedVolumeNames(lbls)
		if err != nil {
			return nil, fmt.Errorf("mounts of container %s: %w", c.ID(), err)
		}
		for _, name := range names {
			out[name] = append(out[name], VolumeMounter{ID: c.ID(), Name: lbls[labels.Name]})
		}
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	}
	return out, nil
}

// mountedVolumeNames returns the distinct volumes a container mounts, from its labels.
func mountedVolumeNames(lbls map[string]string) ([]string, error) {
	raw := labels.GetMount(lbls)
	if raw == "" {
		return nil, nil
	}
	var mounts []dockercompat.MountPoint
	if err := json.Unmarshal([]byte(raw), &mounts); err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for _, m := range mounts {
		if m.Type != mountutil.Volume || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		names = append(names, m.Name)
	}
	return names, nil
}

func validVolumeName(name string) error {
	if err := identifiers.ValidateDockerCompat(name); err != nil {
		return fmt.Errorf("%w: volume name: %w", errdefs.ErrInvalidArgument, err)
	}
	return nil
}

func (cv clientVolumes) Create(_ context.Context, namespace, name string, lbls map[string]string) (Volume, error) {
	if err := validVolumeName(name); err != nil {
		return Volume{}, err
	}
	list := make([]string, 0, len(lbls))
	for k, v := range lbls {
		if k == "" {
			return Volume{}, fmt.Errorf("%w: label keys must not be empty", errdefs.ErrInvalidArgument)
		}
		list = append(list, k+"="+v)
	}
	sort.Strings(list)
	cfg := cv.c.nerdctlFor(namespace)
	vs, err := nvolume.Store(namespace, cfg.DataRoot, cfg.Address)
	if err != nil {
		return Volume{}, fmt.Errorf("volume store: %w", err)
	}
	// nerdctl returns an existing volume of the same name; the UI asks for a new one.
	if _, err := vs.Get(name, false); err == nil {
		return Volume{}, fmt.Errorf("%w: volume %q already exists", errdefs.ErrAlreadyExists, name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return Volume{}, fmt.Errorf("volume %s: %w", name, err)
	}
	if _, err := vs.Create(name, list); err != nil {
		return Volume{}, fmt.Errorf("create volume %s: %w", name, err)
	}
	// The value returned by Create has no labels; read the volume back as stored.
	v, err := vs.Get(name, false)
	if err != nil {
		return Volume{}, fmt.Errorf("volume %s: %w", name, err)
	}
	return toVolume(*v, false, nil), nil
}

func (cv clientVolumes) Remove(ctx context.Context, namespace, name string) error {
	if err := validVolumeName(name); err != nil {
		return err
	}
	cl, err := cv.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	cfg := cv.c.nerdctlFor(namespace)
	vs, err := nvolume.Store(namespace, cfg.DataRoot, cfg.Address)
	if err != nil {
		return fmt.Errorf("volume store: %w", err)
	}
	var inUse []VolumeMounter
	// The usage check runs under the volume store lock, like `nerdctl volume rm`.
	removed, warns, err := vs.Remove(func() ([]string, []error, error) {
		used, err := volumeMounters(ctx, cl)
		if err != nil {
			return nil, nil, err
		}
		if inUse = used[name]; len(inUse) > 0 {
			return nil, nil, nil
		}
		return []string{name}, nil, nil
	})
	if err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
	}
	if len(inUse) > 0 {
		who := inUse[0].Name
		if who == "" {
			who = inUse[0].ID
		}
		return fmt.Errorf("%w: volume %q is used by %d container(s), e.g. %s; remove them first",
			errdefs.ErrFailedPrecondition, name, len(inUse), who)
	}
	if len(removed) == 0 {
		for _, w := range warns {
			if errors.Is(w, store.ErrNotFound) {
				return fmt.Errorf("%w: volume %q not found", errdefs.ErrNotFound, name)
			}
		}
		return fmt.Errorf("remove volume %s: %w", name, errors.Join(warns...))
	}
	return nil
}
