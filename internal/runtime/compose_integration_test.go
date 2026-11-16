// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationCompose loads a compose project like `nerdctl compose` (profiles, .env
// interpolation) and sees its containers by their compose labels (#24).
func TestIntegrationCompose(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	project := "it-stack-" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	compose := `services:
  web:
    image: docker.io/library/alpine:3
    command: ["sleep", "${SLEEP}"]
  worker:
    image: docker.io/library/alpine:3
    command: ["sleep", "600"]
  debug:
    image: docker.io/library/alpine:3
    profiles: ["debug"]
`
	if err := os.WriteFile(file, []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SLEEP=600\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "compose", "-p", project, "-f", file, "down").Run()
	})

	services, err := c.Compose().Services(ctx, DefaultNamespace, project, dir, file)
	slices.Sort(services)
	if err != nil || !slices.Equal(services, []string{"web", "worker"}) {
		t.Fatalf("services: %v, %v", services, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("services: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compose().Services(ctx, DefaultNamespace, project, dir, filepath.Join(dir, "bad.yaml")); !errdefs.IsInvalidArgument(err) {
		t.Errorf("invalid file: %v", err)
	}

	cmd := exec.Command("nerdctl", "compose", "-p", project, "-f", file, "up", "-d", "web")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nerdctl compose up: %v: %s", err, out)
	}
	list, err := c.Containers().List(ctx, DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, ct := range list {
		if ct.Compose != nil && ct.Compose.Project == project {
			found = append(found, ct.Compose.Service+":"+ct.State)
		}
	}
	if !slices.Equal(found, []string{"web:" + StateRunning}) {
		t.Errorf("project containers %v", found)
	}
}
