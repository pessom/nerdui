// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Run with: sudo -E go test -tags integration ./internal/runtime/...
func TestIntegrationVersions(t *testing.T) {
	addr := os.Getenv("NERDUI_TEST_ADDRESS")
	if addr == "" {
		addr = DefaultAddress
	}
	c := New(addr, DefaultNamespace)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	v, err := c.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if v.Containerd == "" {
		t.Fatal("containerd version is empty")
	}
	t.Logf("containerd %s (%s), nerdctl lib %s", v.Containerd, v.ContainerdRevision, v.NerdctlLibrary)

	// CI runs a matrix of supported containerd versions (ADR-0012); make sure the expected
	// daemon is actually the one under test.
	want := os.Getenv("NERDUI_TEST_EXPECT_CONTAINERD")
	t.Logf("NERDUI_TEST_EXPECT_CONTAINERD=%q", want)
	if want != "" && !strings.HasPrefix(v.Containerd, want) {
		t.Fatalf("containerd version = %q, want prefix %q", v.Containerd, want)
	}
}

func TestIntegrationNamespaces(t *testing.T) {
	addr := os.Getenv("NERDUI_TEST_ADDRESS")
	if addr == "" {
		addr = DefaultAddress
	}
	c := New(addr, DefaultNamespace)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// CI runs `nerdctl run` in the default namespace before the tests, so it exists.
	ns, err := c.System().Namespaces(ctx)
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if !slices.Contains(ns, DefaultNamespace) || !slices.IsSorted(ns) {
		t.Errorf("namespaces = %v", ns)
	}
}

// CI installs nerdctl-full with the same minor as the linked packages (ADR-0011).
func TestIntegrationNerdctlCompatible(t *testing.T) {
	c := New(DefaultAddress, DefaultNamespace, WithNerdctl("nerdctl", false))
	st := c.Nerdctl(context.Background())
	if !st.Compatible || !st.Writable() {
		t.Fatalf("host nerdctl not compatible: %+v (linked %s)", st, NerdctlLibraryVersion())
	}
	t.Logf("host nerdctl %s, required %s.x", st.Version, st.Required)
}

// TestIntegrationListContainers cross-checks the list with containers created by the nerdctl
// CLI (ADR-0003: objects created anywhere must look the same).
func TestIntegrationListContainers(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlDataRoot(ncfg.DataRoot))
	t.Cleanup(func() { _ = c.Close() })
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	running, stopped, created := "it-run-"+suffix, "it-stop-"+suffix, "it-new-"+suffix
	nerdctl := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("nerdctl", args...).CombinedOutput(); err != nil {
			t.Fatalf("nerdctl %v: %v: %s", args, err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", running, stopped, created).Run() })

	before := time.Now()
	nerdctl("run", "-d", "--name", running, "-p", "127.0.0.1:18080:80",
		"--label", "com.docker.compose.project=itdemo", "--label", "com.docker.compose.service=web",
		"docker.io/library/alpine:3", "sleep", "300")
	// nerdctl run propagates the container exit code; 3 is expected here.
	if out, err := exec.Command("nerdctl", "run", "--name", stopped,
		"docker.io/library/alpine:3", "sh", "-c", "exit 3").CombinedOutput(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 3 {
			t.Fatalf("nerdctl run %s: %v: %s", stopped, err, out)
		}
	}
	nerdctl("create", "--name", created, "docker.io/library/alpine:3", "true")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := c.Containers().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Container{}
	for _, item := range list {
		byName[item.Name] = item
	}

	r, ok := byName[running]
	if !ok {
		t.Fatalf("%s not listed: %+v", running, list)
	}
	if r.State != StateRunning || r.StartedAt == nil || r.StartedAt.Before(before.Add(-2*time.Second)) {
		t.Errorf("running: state=%s startedAt=%v", r.State, r.StartedAt)
	}
	if r.Compose == nil || r.Compose.Project != "itdemo" || r.Compose.Service != "web" {
		t.Errorf("compose: %+v", r.Compose)
	}
	if len(r.Ports) != 1 || r.Ports[0].HostPort != 18080 || r.Ports[0].ContainerPort != 80 ||
		r.Ports[0].Protocol != "tcp" || r.Ports[0].HostIP != "127.0.0.1" {
		t.Errorf("ports: %+v", r.Ports)
	}
	if r.Image == "" || r.Command == "" || len(r.ID) != 64 {
		t.Errorf("fields: %+v", r)
	}

	s := byName[stopped]
	if s.State != StateExited || s.ExitCode == nil || *s.ExitCode != 3 || s.ExitedAt == nil {
		t.Errorf("stopped: %+v", s)
	}
	if n := byName[created]; n.State != StateCreated {
		t.Errorf("created: %+v", n)
	}
	t.Logf("listed %d containers; running %s started %v", len(list), running, r.StartedAt)
}
