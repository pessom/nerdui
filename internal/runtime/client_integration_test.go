// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os"
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
}
