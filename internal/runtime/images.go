// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
)

// Image is the list representation of an image (#42). Keep in sync with api/openapi.yaml.
type Image struct {
	// Name is the full reference as stored in containerd, e.g. docker.io/library/nginx:1.27.
	Name string `json:"name"`
	// Repository and Tag are empty for images stored under a digest only (dangling).
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	// Digest is the target (index or manifest) digest; images sharing it are the same image.
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
	// Platforms are the platforms whose content is in the store, e.g. linux/amd64.
	Platforms []string `json:"platforms"`
	// Size is the unpacked size on disk (snapshots); ContentSize the stored blobs.
	Size        int64 `json:"size"`
	ContentSize int64 `json:"contentSize"`
	// Containers is the number of containers of the namespace created from this image.
	Containers int               `json:"containers"`
	Labels     map[string]string `json:"labels"`
}

// Images covers image operations of an environment.
type Images interface {
	// List returns the images of a namespace sorted by name.
	List(ctx context.Context, namespace string) ([]Image, error)
}

// Images returns image operations of the local containerd.
func (c *Client) Images() Images { return clientImages{c} }

type clientImages struct{ c *Client }

// imageDigestLabel is the container label nerdctl 2.4+ records with the image target digest at
// creation time (labels.ImageDigest; not defined by nerdctl 2.3, ADR-0017).
const imageDigestLabel = "nerdctl/image-digest"

// List reads images with the containerd client directly, the same way in every nerdctl build
// (#42): sizes in bytes for all platforms in the store, and the number of containers using each
// image (by the digest label, or by image name for containers without it).
func (ci clientImages) List(ctx context.Context, namespace string) ([]Image, error) {
	cl, err := ci.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	list, err := cl.ImageService().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", classify(err))
	}
	used, err := imagesInUse(ctx, cl, list)
	if err != nil {
		return nil, err
	}

	out := make([]Image, len(list))
	keep := make([]bool, len(list))
	sem := make(chan struct{}, listConcurrency)
	var wg sync.WaitGroup
	for i, img := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			item, err := describeImage(ctx, cl, img)
			if err != nil {
				return // removed while listing: skip
			}
			item.Containers = used[img.Target.Digest]
			out[i], keep[i] = item, true
		}()
	}
	wg.Wait()

	result := make([]Image, 0, len(out))
	for i := range out {
		if keep[i] {
			result = append(result, out[i])
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func describeImage(ctx context.Context, cl *containerd.Client, img images.Image) (Image, error) {
	store := cl.ContentStore()
	out := Image{
		Name:      img.Name,
		Digest:    img.Target.Digest.String(),
		CreatedAt: img.CreatedAt.UTC(),
		Platforms: []string{},
		Labels:    img.Labels,
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	out.Repository, out.Tag = splitReference(img.Name)

	all, err := images.Platforms(ctx, store, img.Target)
	if err != nil && !errdefs.IsNotFound(err) {
		return Image{}, err
	}
	seen := map[string]bool{}
	for _, p := range all {
		if p.OS == "unknown" {
			continue // BuildKit attestation manifests (provenance, SBOM)
		}
		name := platforms.Format(platforms.Normalize(p))
		if seen[name] {
			continue
		}
		seen[name] = true
		// An index lists every platform; only those whose content was pulled are on disk.
		available, _, _, missing, err := images.Check(ctx, store, img.Target, platforms.Only(p))
		if err == nil && available && len(missing) == 0 {
			out.Platforms = append(out.Platforms, name)
		}
	}
	sort.Strings(out.Platforms)

	stored := containerd.NewImageWithPlatform(cl, img, platforms.All)
	if out.ContentSize, err = stored.Usage(ctx); err != nil {
		return Image{}, err
	}
	withSnapshots, err := stored.Usage(ctx, containerd.WithSnapshotUsage())
	if err != nil {
		return Image{}, err
	}
	out.Size = withSnapshots - out.ContentSize
	return out, nil
}

// splitReference returns repository and tag of a stored image name. Names that are only a digest
// (dangling images, CRI config IDs) have neither; digest references keep the repository.
func splitReference(name string) (repo, tag string) {
	if _, err := digest.Parse(name); err == nil {
		return "", "" // "sha256:…" would otherwise parse as repository "sha256" with a tag
	}
	ref, err := reference.ParseNormalizedNamed(name)
	if err != nil {
		return "", ""
	}
	repo = reference.FamiliarName(ref)
	if t, ok := ref.(reference.Tagged); ok {
		tag = t.Tag()
	}
	return repo, tag
}

// imagesInUse counts containers per image target digest. Containers created by nerdctl 2.4+
// carry the digest they were created from; others are matched by image name, so a tag moved to
// another image attributes them to the new one (as nerdctl 2.3 does).
func imagesInUse(ctx context.Context, cl *containerd.Client, list []images.Image) (map[digest.Digest]int, error) {
	byName := make(map[string]digest.Digest, len(list))
	for _, img := range list {
		byName[img.Name] = img.Target.Digest
	}
	containers, err := cl.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", classify(err))
	}
	used := map[digest.Digest]int{}
	for _, c := range containers {
		info, err := c.Info(ctx, containerd.WithoutRefreshedMetadata)
		if err != nil {
			continue // removed while listing
		}
		if d, err := digest.Parse(strings.TrimSpace(info.Labels[imageDigestLabel])); err == nil {
			used[d]++
			continue
		}
		if d, ok := byName[info.Image]; ok {
			used[d]++
		}
	}
	return used, nil
}
