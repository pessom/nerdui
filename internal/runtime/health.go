// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"github.com/containerd/nerdctl/v2/pkg/healthcheck"
	"github.com/containerd/nerdctl/v2/pkg/labels"
)

// HealthInfo is a container's healthcheck state as nerdctl records it in its labels.
type HealthInfo struct {
	// Status is starting, healthy or unhealthy.
	Status string
	// FailingStreak counts consecutive failed checks. nerdctl keeps it across restarts.
	FailingStreak int
	// Retries is how many failed checks in a row make the container unhealthy.
	Retries int
}

// ContainerHealth reads the healthcheck state from a container's labels; ok is false for a
// container without a healthcheck or before its first check.
func ContainerHealth(lbls map[string]string) (info HealthInfo, ok bool) {
	if lbls[labels.HealthState] == "" {
		return HealthInfo{}, false
	}
	hs, err := healthcheck.HealthStateFromJSON(lbls[labels.HealthState])
	if err != nil || hs == nil {
		return HealthInfo{}, false
	}
	info = HealthInfo{Status: hs.Status, FailingStreak: hs.FailingStreak, Retries: healthcheck.DefaultProbeRetries}
	if cfg, err := healthcheck.HealthCheckFromJSON(lbls[labels.HealthCheck]); err == nil && cfg != nil && cfg.Retries > 0 {
		info.Retries = cfg.Retries
	}
	return info, true
}
