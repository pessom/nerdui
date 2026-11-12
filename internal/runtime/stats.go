// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/statsutil"
)

// StatsSample is one measurement of a running container (#16), as `nerdctl stats` computes
// it. Keep in sync with api/openapi.yaml.
type StatsSample struct {
	Time time.Time `json:"ts"`
	// CPUPercent is relative to one CPU (200 = two CPUs busy), like docker and nerdctl.
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsage   uint64  `json:"memoryUsage"`
	MemoryLimit   uint64  `json:"memoryLimit"`
	MemoryPercent float64 `json:"memoryPercent"`
	// Network and block I/O are cumulative bytes since the container started.
	NetworkRx  uint64 `json:"networkRx"`
	NetworkTx  uint64 `json:"networkTx"`
	BlockRead  uint64 `json:"blockRead"`
	BlockWrite uint64 `json:"blockWrite"`
	PIDs       uint64 `json:"pids"`
}

// StatsEmitter receives samples; an error stops the sampling.
type StatsEmitter func(StatsSample) error

// Sampling interval bounds (#16).
const (
	MinStatsInterval = time.Second
	MaxStatsInterval = 10 * time.Second
)

// Stats samples the container with the exact id every interval until ctx ends or the
// container stops (then it returns nil). A container that is not running gives a
// failed-precondition error. Sampling only happens while a client listens.
func (cc clientContainers) Stats(ctx context.Context, namespace, id string, interval time.Duration, emit StatsEmitter) error {
	if err := identifiers.Validate(id); err != nil {
		return fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	interval = min(max(interval, MinStatsInterval), MaxStatsInterval)
	cl, err := cc.c.conn()
	if err != nil {
		return err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	ct, err := cl.LoadContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("load container %s: %w", id, classify(err))
	}
	task, err := ct.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("%w: container is not running", errdefs.ErrFailedPrecondition)
		}
		return fmt.Errorf("task of %s: %w", id, classify(err))
	}
	if st, err := task.Status(ctx); err == nil && st.Status != containerd.Running && st.Status != containerd.Paused {
		return fmt.Errorf("%w: container is %s", errdefs.ErrFailedPrecondition, st.Status)
	}

	prev := new(statsutil.ContainerStats)
	// The first measurement only sets the baseline for CPU percent.
	if _, err := sampleTask(ctx, task, prev, true); err != nil {
		return fmt.Errorf("stats of %s: %w", id, err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		// Stopped meanwhile (or the task is gone): the stream ends.
		if st, err := task.Status(ctx); err != nil || (st.Status != containerd.Running && st.Status != containerd.Paused) {
			return nil
		}
		e, err := sampleTask(ctx, task, prev, false)
		if err != nil {
			if st, serr := task.Status(ctx); serr != nil || st.Status == containerd.Stopped {
				return nil
			}
			return fmt.Errorf("stats of %s: %w", id, err)
		}
		if err := emit(StatsSample{
			Time:          time.Now().UTC(),
			CPUPercent:    e.CPUPercentage,
			MemoryUsage:   uint64(e.Memory),
			MemoryLimit:   uint64(e.MemoryLimit),
			MemoryPercent: e.MemoryPercentage,
			NetworkRx:     uint64(e.NetworkRx),
			NetworkTx:     uint64(e.NetworkTx),
			BlockRead:     uint64(e.BlockRead),
			BlockWrite:    uint64(e.BlockWrite),
			PIDs:          e.PidsCurrent,
		}); err != nil {
			return err
		}
	}
}
