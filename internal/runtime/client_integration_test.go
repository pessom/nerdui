// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os"
	"slices"
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
	if want := os.Getenv("NERDUI_TEST_EXPECT_CONTAINERD"); want != "" && !strings.HasPrefix(v.Containerd, want) {
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
