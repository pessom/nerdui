// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package runtime

import (
	"context"
	"fmt"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/statsutil"
)

// sampleTask is only implemented on Linux, where nerdui runs (ADR-0001).
func sampleTask(context.Context, containerd.Task, *statsutil.ContainerStats, bool) (statsutil.StatsEntry, error) {
	return statsutil.StatsEntry{}, fmt.Errorf("%w: container stats need Linux", errdefs.ErrNotImplemented)
}
