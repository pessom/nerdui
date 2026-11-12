// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	ncontainer "github.com/containerd/nerdctl/v2/pkg/cmd/container"
)

// Inspect returns the container in the shape of `nerdctl inspect` (dockercompat mode).
// ref is resolved like nerdctl does: full or short ID, or name. size also reports the
// writable layer and root filesystem sizes, which is slow for large containers.
//
// It calls nerdctl's container.Inspect as is (ADR-0013). That function reports errors as
// plain strings, so a missing container is recognized by its message.
func (cc clientContainers) Inspect(ctx context.Context, namespace, ref string, size bool) (any, error) {
	if err := identifiers.Validate(ref); err != nil {
		return nil, fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := cc.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	cfg := cc.c.nerdctlFor(namespace)
	entries, err := ncontainer.Inspect(ctx, cl, []string{ref}, types.ContainerInspectOptions{
		GOptions: types.GlobalCommandOptions(*cfg),
		Mode:     "dockercompat",
		Size:     size,
	})
	switch {
	case err != nil && strings.Contains(err.Error(), "no such container"):
		return nil, fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, ref)
	case err != nil:
		return nil, fmt.Errorf("inspect %s: %w", ref, classify(err))
	case len(entries) == 0:
		return nil, fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, ref)
	case len(entries) > 1:
		return nil, fmt.Errorf("%w: %q matches %d containers, use the full ID", errdefs.ErrInvalidArgument, ref, len(entries))
	}
	return entries[0], nil
}
