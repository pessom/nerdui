// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-FileCopyrightText: The containerd Authors
// SPDX-License-Identifier: AGPL-3.0-only AND Apache-2.0

// This file adapts unexported code from nerdctl pkg/cmd/container/kill.go
// (https://github.com/containerd/nerdctl, Apache-2.0, Copyright The containerd Authors).

package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/go-cni"
	"github.com/containerd/log"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/config"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	"github.com/containerd/nerdctl/v2/pkg/healthcheck"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/netutil"
	"github.com/containerd/nerdctl/v2/pkg/netutil/nettype"
	"github.com/containerd/nerdctl/v2/pkg/portutil"
	"github.com/containerd/nerdctl/v2/pkg/rootlessutil"
	"github.com/moby/sys/signal"
)

// kill sends SIGKILL like `nerdctl kill`. nerdctl's own Kill calls os.Exit when the task
// disappears, so its steps are reproduced here: mark the container explicitly stopped (so
// the restart manager leaves it alone), release published ports, kill, clean up healthcheck
// units and resume a paused task so the signal is delivered.
func kill(ctx context.Context, c containerd.Container, cfg *config.Config) (err error) {
	defer func() {
		if err != nil {
			_ = containerutil.UpdateErrorLabel(ctx, c, err)
		}
	}()
	sig, err := signal.ParseSignal("SIGKILL")
	if err != nil {
		return err
	}
	if err := containerutil.UpdateExplicitlyStoppedLabel(ctx, c, true); err != nil {
		return err
	}
	if err := cleanupNetwork(ctx, c, cfg); err != nil {
		return fmt.Errorf("clean up network: %w", err)
	}
	task, err := c.Task(ctx, cio.Load)
	if err != nil {
		return err
	}
	status, err := task.Status(ctx)
	if err != nil {
		return err
	}
	paused := status.Status == containerd.Paused || status.Status == containerd.Pausing
	if err := task.Kill(ctx, sig); err != nil {
		return err
	}
	if err := healthcheck.RemoveTransientHealthCheckFiles(ctx, c); err != nil {
		log.G(ctx).WithError(err).Warnf("clean up healthcheck units of %s", c.ID())
	}
	if paused {
		if err := task.Resume(ctx); err != nil {
			log.G(ctx).WithError(err).Warnf("resume %s after kill", c.ID())
		}
	}
	return nil
}

// cleanupNetwork removes the CNI setup (port forwards) of a container before it is killed,
// like `nerdctl kill`. Adapted from nerdctl pkg/cmd/container/kill.go (unexported there).
func cleanupNetwork(ctx context.Context, container containerd.Container, cfg *config.Config) error {
	return rootlessutil.WithDetachedNetNSIfAny(func() error {
		dataStore, err := clientutil.DataStore(cfg.DataRoot, cfg.Address)
		if err != nil {
			return err
		}
		containerLabels, err := container.Labels(ctx)
		if err != nil {
			return err
		}
		ports, err := portutil.LoadPortMappings(dataStore, cfg.Namespace, container.ID(), containerLabels)
		if err != nil {
			return fmt.Errorf("load port mappings: %w", err)
		}
		spec, err := container.Spec(ctx)
		if err != nil {
			return err
		}
		var networks []string
		if err := json.Unmarshal([]byte(spec.Annotations[labels.Networks]), &networks); err != nil {
			log.G(ctx).WithError(err).WithField("container", container.ID()).Info("no networking information")
			return nil
		}
		netType, err := nettype.Detect(networks)
		if err != nil {
			return err
		}
		switch netType {
		case nettype.Host, nettype.None, nettype.Container, nettype.Namespace:
			return nil
		case nettype.CNI:
		default:
			return fmt.Errorf("unexpected network type %v", netType)
		}
		e, err := netutil.NewCNIEnv(cfg.CNIPath, cfg.CNINetConfPath,
			netutil.WithNamespace(cfg.Namespace), netutil.WithDefaultNetwork(cfg.BridgeIP))
		if err != nil {
			return err
		}
		cniOpts := []cni.Opt{cni.WithPluginDir([]string{cfg.CNIPath})}
		for _, name := range networks {
			netw, err := e.NetworkByNameOrID(name)
			if err != nil {
				return err
			}
			cniOpts = append(cniOpts, cni.WithConfListBytes(netw.Bytes))
		}
		cniObj, err := cni.New(cniOpts...)
		if err != nil {
			return err
		}
		fullID := spec.Annotations[labels.Namespace] + "-" + container.ID()
		return cniObj.Remove(ctx, fullID, "", cni.WithCapabilityPortMap(ports))
	})
}
