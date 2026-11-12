// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	nimage "github.com/containerd/nerdctl/v2/pkg/cmd/image"
	"github.com/containerd/nerdctl/v2/pkg/referenceutil"
)

// Image actions use nerdctl's own tag, rmi and push (#44), so each build behaves like the
// nerdctl CLI it is built for (ADR-0017): container checks and force semantics of rmi,
// registry configuration and credentials (docker config) of push.

// PushOptions are the options of an image push (#44).
type PushOptions struct {
	// Platform pushes one platform; empty means the host platform.
	Platform string
	// AllPlatforms pushes every platform in the store.
	AllPlatforms bool
}

// Tag creates target as another reference to source, like `nerdctl tag`.
func (ci clientImages) Tag(ctx context.Context, namespace, source, target string) error {
	for _, ref := range []string{source, target} {
		if err := validRef(ref); err != nil {
			return err
		}
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	cfg := ci.c.nerdctlFor(namespace)
	err = nimage.Tag(ctx, cl, types.ImageTagOptions{
		GOptions: types.GlobalCommandOptions(*cfg), Source: source, Target: target,
	})
	if err != nil {
		return fmt.Errorf("tag %s as %s: %w", source, target, classifyImageErr(err))
	}
	return nil
}

// Remove removes an image reference like `nerdctl rmi`. Without force, images used by
// containers are refused (409); with force, an image used by a running container is only
// untagged, its content stays.
func (ci clientImages) Remove(ctx context.Context, namespace, ref string, force bool) error {
	if ref == "" {
		return fmt.Errorf("%w: image reference is required", errdefs.ErrInvalidArgument)
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	cfg := ci.c.nerdctlFor(namespace)
	err = nimage.Remove(ctx, cl, []string{ref}, types.ImageRemoveOptions{
		Stdout: io.Discard, GOptions: types.GlobalCommandOptions(*cfg), Force: force,
	})
	if err != nil {
		return fmt.Errorf("remove %s: %w", ref, classifyImageErr(err))
	}
	return nil
}

// Push pushes an image like `nerdctl push`, writing progress to out as terminal output.
func (ci clientImages) Push(ctx context.Context, namespace, ref string, opts PushOptions, out io.Writer) error {
	if err := validRef(ref); err != nil {
		return err
	}
	if opts.AllPlatforms && opts.Platform != "" {
		return fmt.Errorf("%w: platform and all platforms are mutually exclusive", errdefs.ErrInvalidArgument)
	}
	cl, err := ci.c.conn()
	if err != nil {
		return err
	}
	ctx = withJobLogger(namespaces.WithNamespace(ctx, namespace), out)
	cfg := ci.c.nerdctlFor(namespace)
	var platforms []string
	if opts.Platform != "" {
		platforms = []string{opts.Platform}
	}
	_, _ = fmt.Fprintf(out, "Pushing %s\r\n", ref)
	err = nimage.Push(ctx, cl, ref, types.ImagePushOptions{
		Stdout:       out,
		GOptions:     types.GlobalCommandOptions(*cfg),
		SignOptions:  types.ImageSignOptions{Provider: "none"},
		Platforms:    platforms,
		AllPlatforms: opts.AllPlatforms,
	})
	if err != nil {
		return fmt.Errorf("push %s: %w", ref, classifyImageErr(err))
	}
	return nil
}

func validRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("%w: image reference is required", errdefs.ErrInvalidArgument)
	}
	if _, err := referenceutil.Parse(ref); err != nil {
		return fmt.Errorf("%w: invalid image reference %q", errdefs.ErrInvalidArgument, ref)
	}
	return nil
}

// classifyImageErr maps nerdctl's plain-text image errors to error classes (as ADR-0013 does
// for inspect): unknown images, conflicts with containers, ambiguous short IDs.
func classifyImageErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such image"), strings.Contains(msg, "does not exist locally"):
		return fmt.Errorf("%w: %w", errdefs.ErrNotFound, err)
	case strings.Contains(msg, "conflict:"):
		return fmt.Errorf("%w: %w", errdefs.ErrConflict, err)
	case strings.Contains(msg, "multiple IDs found"):
		return fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	return classify(err)
}
