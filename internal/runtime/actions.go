// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	ncontainer "github.com/containerd/nerdctl/v2/pkg/cmd/container"
	"github.com/containerd/nerdctl/v2/pkg/config"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
)

// Action is a container lifecycle operation (#11, #12).
type Action string

// Container actions.
const (
	ActionStart   Action = "start"
	ActionStop    Action = "stop"
	ActionRestart Action = "restart"
	ActionKill    Action = "kill"
	ActionPause   Action = "pause"
	ActionUnpause Action = "unpause"
	ActionRemove  Action = "remove"
)

// Actions lists all container actions.
var Actions = []Action{ActionStart, ActionStop, ActionRestart, ActionKill, ActionPause, ActionUnpause, ActionRemove}

// Valid reports whether a is a known action.
func (a Action) Valid() bool { return slices.Contains(Actions, a) }

// ActionOptions tune an action. Force and Volumes apply to remove only.
type ActionOptions struct {
	// Force removes a running or paused container (killing it first).
	Force bool
	// Volumes also removes anonymous volumes of the container.
	Volumes bool
}

// Do performs action on the container with the exact id. Errors are classified with
// errdefs: unknown container → NotFound, wrong state → FailedPrecondition.
func (cc clientContainers) Do(ctx context.Context, namespace, id string, action Action, opts ActionOptions) error {
	if !action.Valid() {
		return fmt.Errorf("%w: unknown action %q", errdefs.ErrInvalidArgument, action)
	}
	if err := identifiers.Validate(id); err != nil {
		return fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := cc.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	c, err := cl.LoadContainer(ctx, id)
	if err != nil {
		if errdefs.IsNotFound(classify(err)) {
			return fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, id)
		}
		return fmt.Errorf("load container: %w", classify(err))
	}
	if err := checkState(ctx, c, action, opts); err != nil {
		return err
	}

	cfg := cc.c.nerdctlFor(namespace)
	nerdctlCmd, nerdctlArgs := cc.c.nerdctlInvocation(cfg)
	switch action {
	case ActionStart:
		err = containerutil.Start(ctx, c, false, false, cl, "", "", cfg, nerdctlCmd, nerdctlArgs)
	case ActionStop:
		err = containerutil.Stop(ctx, c, nil, "")
	case ActionRestart:
		// Same sequence as `nerdctl restart`.
		if err = containerutil.Stop(ctx, c, nil, ""); err == nil {
			err = containerutil.Start(ctx, c, false, false, cl, "", "", cfg, nerdctlCmd, nerdctlArgs)
		}
	case ActionKill:
		err = kill(ctx, c, cfg)
	case ActionPause:
		err = containerutil.Pause(ctx, cl, id)
	case ActionUnpause:
		err = containerutil.Unpause(ctx, cl, id, cfg, nerdctlCmd, nerdctlArgs)
	case ActionRemove:
		err = ncontainer.RemoveContainer(ctx, c, types.GlobalCommandOptions(*cfg), opts.Force, opts.Volumes, cl)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, id, classify(err))
	}
	return nil
}

// checkState rejects actions that do not apply to the container's current state with
// FailedPrecondition, so the API answers 409 with a clear message instead of a raw
// containerd error.
func checkState(ctx context.Context, c containerd.Container, action Action, opts ActionOptions) error {
	st := containerd.Stopped
	task, err := c.Task(ctx, nil)
	switch {
	case err == nil:
		s, err := task.Status(ctx)
		if err != nil && !errdefs.IsNotFound(classify(err)) {
			return fmt.Errorf("task status: %w", classify(err))
		}
		if err == nil {
			st = s.Status
		}
	case !errdefs.IsNotFound(classify(err)):
		return fmt.Errorf("load task: %w", classify(err))
	}
	running := st == containerd.Running
	paused := st == containerd.Paused || st == containerd.Pausing
	var reason string
	switch action {
	case ActionStart:
		if running || paused {
			reason = "is already running"
		}
	case ActionPause:
		if paused {
			reason = "is already paused"
		} else if !running {
			reason = "is not running"
		}
	case ActionKill:
		if !running && !paused {
			reason = "is not running"
		}
	case ActionUnpause:
		if !paused {
			reason = "is not paused"
		}
	case ActionRemove:
		if (running || paused) && !opts.Force {
			reason = "is running: stop it first or force removal"
		}
	case ActionStop, ActionRestart:
		// Stopping a stopped container is a no-op, like nerdctl.
	}
	if reason != "" {
		return fmt.Errorf("%w: container %s %s", errdefs.ErrFailedPrecondition, c.ID(), reason)
	}
	return nil
}

// nerdctlFor returns the nerdctl configuration for calls in namespace.
func (c *Client) nerdctlFor(namespace string) *config.Config {
	cfg := c.nerdctlConfig
	cfg.Address = c.address
	cfg.Namespace = namespace
	return &cfg
}

// nerdctlInvocation is the command nerdctl packages record for later runs, e.g. healthcheck
// timers (`<cmd> <args> container healthcheck <id>`). The binary must be an absolute path for
// systemd-run; global flags select the same containerd, namespace and data root.
func (c *Client) nerdctlInvocation(cfg *config.Config) (string, []string) {
	cmd := c.nerdctl.path
	if p, err := exec.LookPath(cmd); err == nil {
		cmd = p
	}
	return cmd, []string{
		"--address=" + cfg.Address,
		"--namespace=" + cfg.Namespace,
		"--data-root=" + cfg.DataRoot,
	}
}

// actionTimeout bounds a single action; stop waits up to the container's stop timeout
// (10 s by default) before SIGKILL.
const actionTimeout = 2 * time.Minute

// ActionContext detaches an action from the HTTP request: a closed browser tab must not
// abort a stop or remove halfway. The action is still bounded by actionTimeout.
func ActionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), actionTimeout)
}
