// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	ncontainer "github.com/containerd/nerdctl/v2/pkg/cmd/container"
)

// RecreateOptions control Recreate.
type RecreateOptions struct {
	// Pull pulls the image before replacing the container (an update).
	Pull bool
}

// Recreate replaces a container with one created from spec under the same name (#18,
// ADR-0031): the old container is stopped and renamed, the new one created (and started when
// the old one ran), and the old one removed. When creating or starting fails, the new
// container is removed and the old one gets its name back and is started again.
func (cc clientContainers) Recreate(ctx context.Context, namespace, ref string, spec CreateSpec, opts RecreateOptions, out io.Writer) (string, error) {
	cur, err := cc.Spec(ctx, namespace, ref)
	if err != nil {
		return "", err
	}
	if cur.Stack != "" {
		return "", ErrStackContainer
	}
	if spec.Name == "" {
		spec.Name = cur.Spec.Name
	}
	if spec.Name != cur.Spec.Name {
		return "", invalid("the name cannot change when recreating (%s)", cur.Spec.Name)
	}
	if _, err := CreateArgs(spec); err != nil {
		return "", err
	}
	if opts.Pull {
		if err := cc.c.Images().Pull(ctx, namespace, spec.Image, PullOptions{}, out); err != nil {
			return "", err
		}
	}
	cl, err := cc.c.conn()
	if err != nil {
		return "", err
	}
	nctx := namespaces.WithNamespace(ctx, namespace)
	gopts := types.GlobalCommandOptions(*cc.c.nerdctlFor(namespace))
	oldID := cur.ID

	// Cleanup and rollback must finish even when the request is canceled.
	bg := context.WithoutCancel(ctx)
	if cur.Running {
		_, _ = fmt.Fprintf(out, "\x1b[1mStopping %s\x1b[0m\r\n", spec.Name)
		if err := cc.Do(ctx, namespace, oldID, ActionStop, ActionOptions{}); err != nil {
			return "", err
		}
	}
	parked := spec.Name + "-nerdui-old-" + strconv.FormatInt(time.Now().Unix(), 36)
	if len(parked) > 128 {
		parked = "nerdui-old-" + oldID[:12]
	}
	if err := ncontainer.Rename(nctx, cl, oldID, parked, types.ContainerRenameOptions{Stdout: io.Discard, GOptions: gopts}); err != nil {
		cc.restart(bg, namespace, oldID, cur.Running, out)
		return "", fmt.Errorf("rename %s: %w", spec.Name, classify(err))
	}

	newID, err := cc.Create(ctx, namespace, spec, cur.Running, out)
	if err != nil {
		_, _ = fmt.Fprintf(out, "\r\n\x1b[1;31mRecreating failed: %v\x1b[0m\r\n\x1b[1mRestoring the previous container\x1b[0m\r\n", err)
		if newID != "" {
			_ = cc.Do(bg, namespace, newID, ActionRemove, ActionOptions{Force: true})
		}
		if rerr := ncontainer.Rename(namespaces.WithNamespace(bg, namespace), cl, oldID, spec.Name, types.ContainerRenameOptions{Stdout: io.Discard, GOptions: gopts}); rerr != nil {
			return "", fmt.Errorf("%w; restoring the name failed too (%w): the previous container is %s", err, rerr, parked)
		}
		cc.restart(bg, namespace, oldID, cur.Running, out)
		return "", fmt.Errorf("%w (the previous container was restored)", err)
	}

	_, _ = fmt.Fprintf(out, "\x1b[1mRemoving the previous container\x1b[0m\r\n")
	if err := cc.Do(bg, namespace, oldID, ActionRemove, ActionOptions{Force: true}); err != nil {
		_, _ = fmt.Fprintf(out, "Could not remove %s (%s): %v\r\n", parked, oldID[:12], err)
	}
	return newID, nil
}

// restart starts a container again when it ran before a failed recreate.
func (cc clientContainers) restart(ctx context.Context, namespace, id string, running bool, out io.Writer) {
	if !running {
		return
	}
	if err := cc.Do(ctx, namespace, id, ActionStart, ActionOptions{}); err != nil && !errdefs.IsFailedPrecondition(err) {
		_, _ = fmt.Fprintf(out, "Could not start the previous container again: %v\r\n", err)
	}
}
