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
)

// TestIntegrationPrune previews and prunes stopped containers (#52). Images are only previewed
// with "all unused" and pruned as dangling, so images other tests use stay.
func TestIntegrationPrune(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const unused = "docker.io/library/busybox:1.36"
	if out, err := exec.Command("nerdctl", "pull", "-q", unused).CombinedOutput(); err != nil {
		t.Fatalf("pull: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rmi", "-f", unused).Run() })
	name := "it-prune-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	if out, err := exec.Command("nerdctl", "run", "--name", name, "docker.io/library/alpine:3", "true").CombinedOutput(); err != nil {
		t.Fatalf("run: %v: %s", err, out)
	}

	p, err := c.PrunePreview(ctx, DefaultNamespace, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(p.Containers, func(i PrunedItem) bool { return i.Name == name }) {
		t.Errorf("stopped container %s not in preview %+v", name, p.Containers)
	}
	if !slices.ContainsFunc(p.Images, func(i PrunedItem) bool { return i.Name == unused && i.Size > 0 }) {
		t.Errorf("unused image %s not in preview %+v", unused, p.Images)
	}
	if slices.ContainsFunc(p.Images, func(i PrunedItem) bool { return i.Name == "docker.io/library/alpine:3" }) {
		t.Error("image of an existing container in the preview")
	}

	var out bytes.Buffer
	if err := c.Prune(ctx, DefaultNamespace, PruneOptions{Containers: true, Images: true, BuildCache: true}, &out); err != nil {
		t.Fatalf("prune: %v\n%s", err, out.String())
	}
	list, err := c.Containers().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(list, func(ct Container) bool { return ct.Name == name }) {
		t.Errorf("stopped container %s not pruned:\n%s", name, out.String())
	}
	if !strings.Contains(out.String(), "Build cache") {
		t.Errorf("no build cache section:\n%s", out.String())
	}
	images, err := c.Images().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(images, func(i Image) bool { return i.Name == unused }) {
		t.Error("dangling-only prune removed a tagged image")
	}
}
