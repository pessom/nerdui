// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package runtime

import (
	"fmt"

	"github.com/containerd/errdefs"
)

var procRoot = "/proc"

// readProcess is only implemented on Linux, where nerdui runs (ADR-0001).
func readProcess(string, int, map[int]string) (Process, error) {
	return Process{}, fmt.Errorf("%w: container processes need Linux", errdefs.ErrNotImplemented)
}
