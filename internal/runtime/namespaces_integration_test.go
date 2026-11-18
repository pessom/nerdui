// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationNamespaceLifecycle creates a namespace, refuses to remove it while it holds a nerdctl
// volume (unknown to containerd) and removes it once empty (#195).
func TestIntegrationNamespaceLifecycle(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ns := "it-ns-" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "-n", ns, "volume", "rm", "-f", "data").Run()
		_ = exec.Command("nerdctl", "namespace", "rm", ns).Run()
	})

	if err := c.CreateNamespace(ctx, "bad name"); !errdefs.IsInvalidArgument(err) {
		t.Errorf("invalid name: %v", err)
	}
	if err := c.CreateNamespace(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateNamespace(ctx, ns); !errdefs.IsAlreadyExists(err) {
		t.Errorf("create twice: %v", err)
	}
	nss, err := c.Namespaces(ctx)
	if err != nil || !slices.Contains(nss, ns) {
		t.Fatalf("namespaces %v: %v", nss, err)
	}

	if out, err := exec.Command("nerdctl", "-n", ns, "volume", "create", "data").CombinedOutput(); err != nil {
		t.Fatalf("volume create: %v: %s", err, out)
	}
	err = c.RemoveNamespace(ctx, ns)
	if !errdefs.IsFailedPrecondition(err) || !strings.Contains(err.Error(), "1 volume") {
		t.Fatalf("remove with a volume: %v", err)
	}
	if out, err := exec.Command("nerdctl", "-n", ns, "volume", "rm", "data").CombinedOutput(); err != nil {
		t.Fatalf("volume rm: %v: %s", err, out)
	}

	if err := c.RemoveNamespace(ctx, ns); err != nil {
		t.Fatalf("remove empty: %v", err)
	}
	if err := c.RemoveNamespace(ctx, ns); !errdefs.IsNotFound(err) {
		t.Errorf("remove twice: %v", err)
	}
	out, err := exec.Command("nerdctl", "namespace", "ls", "-q").Output()
	if err != nil || slices.Contains(strings.Fields(string(out)), ns) {
		t.Errorf("nerdctl namespace ls still lists %s: %v %s", ns, err, out)
	}
}
