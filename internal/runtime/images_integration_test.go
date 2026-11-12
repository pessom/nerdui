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

	"github.com/containerd/errdefs"
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

// TestIntegrationPull pulls a small image with progress output and finds it in the list (#43).
func TestIntegrationPull(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	const ref = "docker.io/library/busybox:1.37"
	_ = exec.Command("nerdctl", "rmi", "-f", ref).Run()
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rmi", "-f", ref).Run() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var out bytes.Buffer
	if err := c.Images().Pull(ctx, DefaultNamespace, "busybox:1.37", PullOptions{}, &out); err != nil {
		t.Fatalf("pull: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("elapsed")) {
		t.Errorf("no progress in output:\n%s", out.String())
	}
	list, err := c.Images().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list, func(i Image) bool { return i.Name == ref && i.Size > 0 }) {
		t.Errorf("%s not listed after pull", ref)
	}

	if err := c.Images().Pull(ctx, DefaultNamespace, "invalid reference!", PullOptions{}, &out); err == nil {
		t.Error("invalid reference accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := c.Images().Pull(canceled, DefaultNamespace, "docker.io/library/alpine:3.20", PullOptions{}, &out); err == nil {
		t.Error("pull with a canceled context succeeded")
	}
}

// TestIntegrationImageActions tags, pushes to a local registry and removes images (#44).
func TestIntegrationImageActions(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	ncfg.InsecureRegistry = true // the local registry below speaks plain HTTP
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	img := c.Images()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	const source = "docker.io/library/alpine:3"
	if out, err := exec.Command("nerdctl", "pull", "-q", source).CombinedOutput(); err != nil {
		t.Fatalf("nerdctl pull: %v: %s", err, out)
	}

	// Tag.
	tagged := "localhost:5000/nerdui-it/alpine:" + suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rmi", "-f", tagged).Run() })
	if err := img.Tag(ctx, DefaultNamespace, source, tagged); err != nil {
		t.Fatal(err)
	}
	if err := img.Tag(ctx, DefaultNamespace, "nerdui-it/no-such-image:1", "x:1"); err == nil {
		t.Error("tagging a missing image succeeded")
	}

	// Push to a local registry.
	reg := "it-reg-" + suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", reg).Run() })
	if out, err := exec.Command("nerdctl", "run", "-d", "--name", reg, "-p", "127.0.0.1:5000:5000", "docker.io/library/registry:2").CombinedOutput(); err != nil {
		t.Fatalf("start registry: %v: %s", err, out)
	}
	var out bytes.Buffer
	var pushErr error
	for range 20 { // the registry needs a moment to listen
		out.Reset()
		if pushErr = img.Push(ctx, DefaultNamespace, tagged, PushOptions{}, &out); pushErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if pushErr != nil {
		t.Fatalf("push: %v\n%s", pushErr, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("elapsed")) {
		t.Errorf("no progress in push output:\n%s", out.String())
	}

	// Remove: refused while a container uses the image, then forced.
	name := "it-rmi-" + suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	if out, err := exec.Command("nerdctl", "create", "--name", name, tagged, "true").CombinedOutput(); err != nil {
		t.Fatalf("nerdctl create: %v: %s", err, out)
	}
	if err := img.Remove(ctx, DefaultNamespace, tagged, false); !errdefs.IsConflict(err) {
		t.Errorf("remove image used by a container: %v", err)
	}
	if err := img.Remove(ctx, DefaultNamespace, tagged, true); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if err := img.Remove(ctx, DefaultNamespace, tagged, false); !errdefs.IsNotFound(err) {
		t.Errorf("remove a removed image: %v", err)
	}
}
