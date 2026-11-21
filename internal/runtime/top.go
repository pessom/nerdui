// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"sort"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
)

// Process is a process of a container as the host sees it (#20). Keep in sync with
// api/openapi.yaml.
type Process struct {
	PID  int `json:"pid"`
	PPID int `json:"ppid"`
	// UID is the real user ID in the host's view; User its name on the host, when known.
	UID  int    `json:"uid"`
	User string `json:"user,omitempty"`
	// State is the kernel state letter: R running, S sleeping, D disk sleep, Z zombie, …
	State string `json:"state"`
	// CPU is the share of one CPU used since the process started, in percent (like ps).
	CPU float64 `json:"cpu"`
	// RSS is the resident memory in bytes.
	RSS     int64     `json:"rss"`
	Started time.Time `json:"started"`
	Command string    `json:"command"`
}

// Top lists the processes of a running container, ordered by PID. nerdui runs on the host
// (ADR-0003), so it reads them from /proc instead of running the host's ps, which may be
// missing.
func (cc clientContainers) Top(ctx context.Context, namespace, id string) ([]Process, error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	cl, err := cc.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	ct, err := cl.LoadContainer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load container %s: %w", id, classify(err))
	}
	task, err := ct.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("%w: container is not running", errdefs.ErrFailedPrecondition)
		}
		return nil, fmt.Errorf("task of %s: %w", id, classify(err))
	}
	if st, err := task.Status(ctx); err == nil && st.Status != containerd.Running && st.Status != containerd.Paused {
		return nil, fmt.Errorf("%w: container is %s", errdefs.ErrFailedPrecondition, st.Status)
	}
	pids, err := task.Pids(ctx)
	if err != nil {
		return nil, fmt.Errorf("processes of %s: %w", id, classify(err))
	}
	list := make([]int, 0, len(pids))
	for _, p := range pids {
		list = append(list, int(p.Pid))
	}
	return readProcesses(list)
}

// readProcesses reads processes from /proc, skipping those that exited meanwhile.
func readProcesses(pids []int) ([]Process, error) {
	sort.Ints(pids)
	out := make([]Process, 0, len(pids))
	users := map[int]string{}
	for _, pid := range pids {
		p, err := readProcess(procRoot, pid, users)
		if err != nil {
			if errdefs.IsNotImplemented(err) {
				return nil, err
			}
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
