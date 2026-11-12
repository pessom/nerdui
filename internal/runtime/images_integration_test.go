// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/containerd/platforms"
)

// TestIntegrationImages compares the image list with `nerdctl images` and checks sizes,
// platforms and the in-use count (#42).
func TestIntegrationImages(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	const ref = "docker.io/library/alpine:3"
	if out, err := exec.Command("nerdctl", "pull", "-q", ref).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl pull: %v: %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	find := func() Image {
		t.Helper()
		list, err := c.Images().List(ctx, DefaultNamespace)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(list, func(img Image) bool { return img.Name == ref })
		if i < 0 {
			t.Fatalf("%s not listed in %d images", ref, len(list))
		}
		return list[i]
	}
	before := find()

	out, err := exec.Command("nerdctl", "images", "--no-trunc", "--format", "{{json .}}").Output()
	if err != nil {
		t.Fatalf("nerdctl images: %v", err)
	}
	var cliDigest string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var row struct{ Repository, Tag, Digest string }
		if json.Unmarshal(sc.Bytes(), &row) == nil && row.Repository == "alpine" && row.Tag == "3" {
			cliDigest = row.Digest
		}
	}
	if before.Digest != cliDigest || before.Repository != "alpine" || before.Tag != "3" {
		t.Errorf("image %+v, nerdctl images digest %q", before, cliDigest)
	}
	host := platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	if !slices.Contains(before.Platforms, host) || before.Size <= 0 || before.ContentSize <= 0 {
		t.Errorf("platforms %v (host %s), size %d, content size %d", before.Platforms, host, before.Size, before.ContentSize)
	}

	name := "it-img-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	if out, err := exec.Command("nerdctl", "create", "--name", name, ref, "true").CombinedOutput(); err != nil {
		t.Fatalf("nerdctl create: %v: %s", err, out)
	}
	if after := find(); after.Containers != before.Containers+1 {
		t.Errorf("containers using %s: %d before, %d after creating one", ref, before.Containers, after.Containers)
	}
}
