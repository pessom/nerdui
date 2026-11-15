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

// TestIntegrationVolumes creates a volume, sees it mounted by a container, refuses to remove it
// while the container exists and removes it afterwards (#57).
func TestIntegrationVolumes(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	vol, ctr := "it-vol-"+suffix, "it-vol-ctr-"+suffix
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "rm", "-f", ctr).Run()
		_ = exec.Command("nerdctl", "volume", "rm", vol).Run()
	})

	created, err := c.Volumes().Create(ctx, DefaultNamespace, vol, map[string]string{"app": "it"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != vol || created.Labels["app"] != "it" || created.Mountpoint == "" {
		t.Errorf("created %+v", created)
	}
	if _, err := c.Volumes().Create(ctx, DefaultNamespace, vol, nil); !errdefs.IsAlreadyExists(err) {
		t.Errorf("create again: %v", err)
	}
	if _, err := c.Volumes().Create(ctx, DefaultNamespace, "bad/name", nil); !errdefs.IsInvalidArgument(err) {
		t.Errorf("bad name: %v", err)
	}
	// nerdctl sees the volume nerdui created.
	if out, err := exec.Command("nerdctl", "volume", "inspect", vol).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl volume inspect: %v: %s", err, out)
	}

	out, err := exec.Command("nerdctl", "run", "-d", "--name", ctr, "-v", vol+":/data",
		"docker.io/library/alpine:3", "sh", "-c", "head -c 1048576 /dev/zero > /data/f && sleep 600").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))

	find := func(size bool) Volume {
		t.Helper()
		list, err := c.Volumes().List(ctx, DefaultNamespace, size)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, v := range list {
			if v.Name == vol {
				return v
			}
		}
		t.Fatalf("volume %s not listed", vol)
		return Volume{}
	}
	v := find(false)
	if len(v.Containers) != 1 || v.Containers[0].ID != id || v.Containers[0].Name != ctr || v.Size != nil {
		t.Errorf("listed %+v", v)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		v = find(true)
		if (v.Size != nil && *v.Size >= 1<<20) || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if v.Size == nil || *v.Size < 1<<20 {
		t.Errorf("size of a volume with a 1 MiB file: %v", v.Size)
	}

	if err := c.Volumes().Remove(ctx, DefaultNamespace, vol); !errdefs.IsFailedPrecondition(err) {
		t.Fatalf("remove used volume: %v", err)
	}
	// A stopped container still holds the volume.
	if out, err := exec.Command("nerdctl", "stop", "-t", "0", ctr).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl stop: %v: %s", err, out)
	}
	if err := c.Volumes().Remove(ctx, DefaultNamespace, vol); !errdefs.IsFailedPrecondition(err) {
		t.Fatalf("remove volume of a stopped container: %v", err)
	}
	if out, err := exec.Command("nerdctl", "rm", ctr).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl rm: %v: %s", err, out)
	}
	if err := c.Volumes().Remove(ctx, DefaultNamespace, vol); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := c.Volumes().Remove(ctx, DefaultNamespace, vol); !errdefs.IsNotFound(err) {
		t.Errorf("remove again: %v", err)
	}
}
