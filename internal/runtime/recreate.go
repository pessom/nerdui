// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	ncontainer "github.com/containerd/nerdctl/v2/pkg/cmd/container"
	"github.com/containerd/nerdctl/v2/pkg/store"
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
	// A stopped task keeps its network (CNI port mappings in iptables, which nerdctl counts as
	// allocated ports) until it is deleted: deleting it runs nerdctl's post-stop hook, which
	// tears the network down. Starting a container deletes the old task anyway.
	if c, err := cl.LoadContainer(nctx, oldID); err == nil {
		if t, err := c.Task(nctx, nil); err == nil {
			if _, err := t.Delete(nctx); err != nil && !errdefs.IsNotFound(err) {
				_, _ = fmt.Fprintf(out, "Could not release the previous container's network: %v\r\n", err)
			}
		}
	}
	parked := spec.Name + "-nerdui-old-" + strconv.FormatInt(time.Now().Unix(), 36)
	if len(parked) > 128 {
		parked = "nerdui-old-" + oldID[:12]
	}
	if err := cc.rename(nctx, cl, oldID, parked, gopts); err != nil {
		cc.restart(bg, namespace, oldID, cur.Running, out)
		return "", fmt.Errorf("rename %s: %w", spec.Name, classify(err))
	}

	// Published host ports must be free before nerdctl allocates them again.
	if !waitPortsFree(ctx, cur.Spec.Ports, 10*time.Second, out) {
		if c, err := cl.LoadContainer(nctx, oldID); err == nil {
			if t, err := c.Task(nctx, nil); err == nil {
				if st, err := t.Status(nctx); err == nil {
					_, _ = fmt.Fprintf(out, "The previous container's task is %s\r\n", st.Status)
				}
			} else {
				_, _ = fmt.Fprintf(out, "The previous container has no task: %v\r\n", err)
			}
		}
	}
	newID, err := cc.Create(ctx, namespace, spec, cur.Running, out)
	if err != nil {
		_, _ = fmt.Fprintf(out, "\r\n\x1b[1;31mRecreating failed: %v\x1b[0m\r\n\x1b[1mRestoring the previous container\x1b[0m\r\n", err)
		if newID != "" {
			_ = cc.Do(bg, namespace, newID, ActionRemove, ActionOptions{Force: true})
		}
		if rerr := cc.rename(namespaces.WithNamespace(bg, namespace), cl, oldID, spec.Name, gopts); rerr != nil {
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

// waitPortsFree waits until the published host ports of the old container can be bound,
// at most timeout, and reports those still in use.
func waitPortsFree(ctx context.Context, ports []PortSpec, timeout time.Duration, out io.Writer) bool {
	deadline := time.Now().Add(timeout)
	free := true
	for _, p := range ports {
		if p.HostPort == 0 {
			continue
		}
		addr := net.JoinHostPort(p.HostIP, strconv.Itoa(p.HostPort))
		for {
			err := tryBind(ctx, p.Protocol, addr)
			if err == nil {
				break
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				_, _ = fmt.Fprintf(out, "Port %s/%s is still in use: %v\r\n", addr, protoOr(p.Protocol), err)
				free = false
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return free
}

func protoOr(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}

// tryBind binds and releases an address.
func tryBind(ctx context.Context, proto, addr string) error {
	var lc net.ListenConfig
	if proto == "udp" {
		c, err := lc.ListenPacket(ctx, "udp", addr)
		if err == nil {
			_ = c.Close()
		}
		return err
	}
	l, err := lc.Listen(ctx, "tcp", addr)
	if err == nil {
		_ = l.Close()
	}
	return err
}

// rename renames a container like `nerdctl rename`. Deleting a stopped task runs nerdctl's
// post-stop hook, which already released the name in nerdctl's name store (starting the
// container acquires it again from the OCI spec); then only the name label changes.
func (cc clientContainers) rename(ctx context.Context, cl *containerd.Client, id, name string, gopts types.GlobalCommandOptions) error {
	err := ncontainer.Rename(ctx, cl, id, name, types.ContainerRenameOptions{Stdout: io.Discard, GOptions: gopts})
	if err == nil || !errors.Is(err, store.ErrNotFound) && !strings.Contains(err.Error(), "does not exist") {
		return err
	}
	c, lerr := cl.LoadContainer(ctx, id)
	if lerr != nil {
		return lerr
	}
	_, lerr = c.SetLabels(ctx, map[string]string{"nerdctl/name": name})
	return lerr
}
