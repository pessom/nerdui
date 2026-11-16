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
)

// TestIntegrationSummary counts a running container and finds the data filesystems (#63).
func TestIntegrationSummary(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "it-summary-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	if out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "docker.io/library/alpine:3", "sleep", "600").CombinedOutput(); err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}

	s, err := c.Summary(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Containers.Running < 1 || s.Images < 1 || s.ImagesSize <= 0 || s.VolumesSize == nil || len(s.Namespaces) == 0 {
		t.Errorf("summary %+v", s)
	}
	if len(s.Filesystems) == 0 || s.Filesystems[0].Total == 0 || len(s.Filesystems[0].Paths) == 0 {
		t.Errorf("filesystems %+v", s.Filesystems)
	}
	// containerd's root once, not each plugin's directory.
	for _, f := range s.Filesystems {
		for _, p := range f.Paths {
			if strings.Contains(p, "io.containerd.") {
				t.Errorf("plugin directory listed: %s", p)
			}
		}
	}
	var sum ContainerCounts
	for _, n := range s.Namespaces {
		sum.add(n.Containers)
	}
	if sum != s.Containers {
		t.Errorf("namespace counts %+v, total %+v", sum, s.Containers)
	}
	t.Logf("summary: containers %+v, images %d (%d bytes), volumes %d, networks %d, filesystems %+v",
		s.Containers, s.Images, s.ImagesSize, s.Volumes, s.Networks, s.Filesystems)
}
