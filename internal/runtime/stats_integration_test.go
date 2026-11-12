// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationStats samples a busy container, ends the stream when it stops and refuses
// stopped containers (#16).
func TestIntegrationStats(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	name := "it-stats-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "--memory", "64m",
		"docker.io/library/alpine:3", "sh", "-c", "while :; do :; done").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var samples []StatsSample
	err = c.Containers().Stats(ctx, DefaultNamespace, id, time.Second, func(s StatsSample) error {
		samples = append(samples, s)
		if len(samples) == 2 {
			// Stop the container: the stream must end by itself.
			go func() { _ = exec.Command("nerdctl", "stop", "-t", "0", name).Run() }()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(samples) < 2 {
		t.Fatalf("samples %+v", samples)
	}
	s := samples[1]
	if s.CPUPercent <= 10 || s.MemoryUsage == 0 || s.MemoryLimit != 64<<20 || s.PIDs < 1 {
		t.Errorf("sample of a busy container with a 64 MiB limit: %+v", s)
	}

	err = c.Containers().Stats(ctx, DefaultNamespace, id, time.Second, func(StatsSample) error { return nil })
	if !errdefs.IsFailedPrecondition(err) {
		t.Errorf("stopped container: %v", err)
	}
}
