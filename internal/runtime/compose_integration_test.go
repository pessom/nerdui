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
	"strings"
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
	// The project is loaded like `nerdctl compose` in its directory: a broken compose file there
	// is an invalid argument.
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "compose.yaml"), []byte("services: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compose().Services(ctx, DefaultNamespace, project, bad, filepath.Join(bad, "compose.yaml")); !errdefs.IsInvalidArgument(err) {
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

// TestIntegrationComposeRun runs stack operations with the host's nerdctl compose (#28).
func TestIntegrationComposeRun(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	project := "it-run-" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	dir := t.TempDir()
	compose := `services:
  web:
    image: docker.io/library/alpine:3
    command: ["sleep", "600"]
    volumes: [data:/data]
volumes:
  data: {}
`
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("nerdctl", "compose", "-p", project, "--project-directory", dir, "down", "-v").Run()
	})

	states := func() []string {
		t.Helper()
		list, err := c.Containers().List(ctx, DefaultNamespace)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, ct := range list {
			if ct.Compose != nil && ct.Compose.Project == project {
				out = append(out, ct.Compose.Service+":"+ct.State)
			}
		}
		return out
	}
	volume := func() bool {
		vols, err := c.Volumes().List(ctx, DefaultNamespace, false)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(vols, func(v Volume) bool { return v.Name == project+"_data" })
	}
	run := func(op string, opts ComposeOptions) string {
		t.Helper()
		var out strings.Builder
		if err := c.Compose().Run(ctx, DefaultNamespace, project, dir, op, opts, &out); err != nil {
			t.Fatalf("%s: %v\n%s", op, err, out.String())
		}
		return out.String()
	}

	if out := run(ComposeUp, ComposeOptions{}); !strings.Contains(out, "nerdctl compose") {
		t.Errorf("up output %q", out)
	}
	if got := states(); !slices.Equal(got, []string{"web:" + StateRunning}) || !volume() {
		t.Fatalf("after up: %v, volume %v", got, volume())
	}
	run(ComposeStop, ComposeOptions{})
	if got := states(); len(got) != 1 || got[0] == "web:"+StateRunning {
		t.Errorf("after stop: %v", got)
	}
	run(ComposeStart, ComposeOptions{})
	if got := states(); !slices.Equal(got, []string{"web:" + StateRunning}) {
		t.Errorf("after start: %v", got)
	}
	// Automatic updates pull and redeploy only the opted-in services (#54).
	run(ComposePull, ComposeOptions{Services: []string{"web"}})
	run(ComposeUp, ComposeOptions{Services: []string{"web"}})
	if got := states(); !slices.Equal(got, []string{"web:" + StateRunning}) {
		t.Errorf("after up web: %v", got)
	}
	run(ComposeDown, ComposeOptions{})
	if got := states(); len(got) != 0 || !volume() {
		t.Errorf("after down: %v, volume kept %v", got, volume())
	}
	run(ComposeUp, ComposeOptions{})
	run(ComposeDown, ComposeOptions{Volumes: true})
	if got := states(); len(got) != 0 || volume() {
		t.Errorf("after down -v: %v, volume %v", got, volume())
	}

	// A failing operation reports nerdctl's exit code.
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  web:\n    image: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := c.Compose().Run(ctx, DefaultNamespace, project, dir, ComposeUp, ComposeOptions{}, &out); err == nil || !strings.Contains(err.Error(), "exit code") {
		t.Errorf("failing up: %v\n%s", err, out.String())
	}
}
