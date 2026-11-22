// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationSaveLoad saves an image as a tar archive, removes it and loads it back (#46).
func TestIntegrationSaveLoad(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg), WithNerdctl("nerdctl", false))
	t.Cleanup(func() { _ = c.Close() })
	ns := "it-archive-" + strconv.FormatInt(time.Now().UnixNano()%1e8, 36)
	const ref = "docker.io/library/alpine:3"
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "-n", ns, "rmi", "-f", ref).Run()
		out, _ := exec.Command("nerdctl", "-n", ns, "images", "-q", "-a").Output()
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("nerdctl", "-n", ns, "rmi", "-f", id).Run()
		}
		_ = exec.Command("nerdctl", "namespace", "remove", ns).Run()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if out, err := exec.Command("nerdctl", "-n", ns, "pull", "-q", ref).CombinedOutput(); err != nil {
		t.Fatalf("pull: %v: %s", err, out)
	}

	images := c.Images()
	if err := images.CheckSave(ctx, ns, []string{"docker.io/library/missing:1"}); !errdefs.IsNotFound(err) {
		t.Errorf("missing image: %v", err)
	}
	var archive bytes.Buffer
	if err := images.Save(ctx, ns, []string{ref}, &archive); err != nil {
		t.Fatal(err)
	}
	if archive.Len() < 1<<20 {
		t.Fatalf("archive of %d bytes", archive.Len())
	}
	if out, err := exec.Command("nerdctl", "-n", ns, "rmi", ref).CombinedOutput(); err != nil {
		t.Fatalf("rmi: %v: %s", err, out)
	}

	names, err := images.Load(ctx, ns, bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, ref) {
		t.Errorf("loaded %v, want %s", names, ref)
	}
	if out, err := exec.Command("nerdctl", "-n", ns, "run", "--rm", "--net=none", ref, "true").CombinedOutput(); err != nil {
		t.Errorf("run the loaded image: %v: %s", err, out)
	}
	if _, err := images.Load(ctx, ns, strings.NewReader("not a tar archive")); !errdefs.IsInvalidArgument(err) {
		t.Errorf("garbage: %v", err)
	}
}
