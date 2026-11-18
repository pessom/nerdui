// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestIntegrationRemoteDigest resolves a tag in Docker Hub and finds the digest nerdctl pulled
// (#53).
func TestIntegrationRemoteDigest(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const ref = "docker.io/library/alpine:3"
	if out, err := exec.Command("nerdctl", "pull", "-q", ref).CombinedOutput(); err != nil {
		t.Fatalf("pull: %v: %s", err, out)
	}
	list, err := c.Images().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	var local string
	for _, img := range list {
		if img.Name == ref {
			local = img.Digest
		}
	}
	remote, err := c.Images().RemoteDigest(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: local %s, registry %s", ref, local, remote)
	if remote != local {
		t.Errorf("registry digest %s, pulled %s", remote, local)
	}

	if _, err := c.Images().RemoteDigest(ctx, "docker.io/library/alpine@"+remote); err == nil {
		t.Error("digest reference resolved")
	}
	if _, err := c.Images().RemoteDigest(ctx, "docker.io/pessom/nerdui-does-not-exist:1"); err == nil {
		t.Error("missing repository resolved")
	}
}
