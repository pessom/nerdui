// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

// TestIntegrationContainerActions drives a container created by the nerdctl CLI through all
// actions and checks the state nerdui and nerdctl see afterwards (#11, #12).
func TestIntegrationContainerActions(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	cc := c.Containers()
	name := "it-act-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", "-v", name).Run() })

	nerdctl := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("nerdctl", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("nerdctl %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// An anonymous volume (-v /data) checks remove with volumes.
	id := nerdctl("run", "-d", "--name", name, "-v", "/data", "-p", "127.0.0.1:18081:80",
		"docker.io/library/alpine:3", "sleep", "300")
	volume := nerdctl("inspect", "--format", "{{range .Mounts}}{{.Name}}{{end}}", id)
	if volume == "" {
		t.Fatal("no anonymous volume")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	state := func() Container {
		t.Helper()
		list, err := cc.List(ctx, DefaultNamespace)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range list {
			if item.ID == id {
				return item
			}
		}
		return Container{State: "removed"}
	}
	do := func(a Action, opts ActionOptions, wantState string) {
		t.Helper()
		if err := cc.Do(ctx, DefaultNamespace, id, a, opts); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		if got := state(); got.State != wantState {
			t.Fatalf("after %s: state %s, want %s", a, got.State, wantState)
		}
	}
	precondition := func(a Action, opts ActionOptions) {
		t.Helper()
		if err := cc.Do(ctx, DefaultNamespace, id, a, opts); !errdefs.IsFailedPrecondition(err) {
			t.Errorf("%s in state %s: err = %v, want FailedPrecondition", a, state().State, err)
		}
	}

	precondition(ActionStart, ActionOptions{})
	precondition(ActionUnpause, ActionOptions{})
	do(ActionPause, ActionOptions{}, StatePaused)
	precondition(ActionPause, ActionOptions{})
	do(ActionUnpause, ActionOptions{}, StateRunning)

	first := state().StartedAt
	do(ActionRestart, ActionOptions{}, StateRunning)
	if s := state().StartedAt; first == nil || s == nil || !s.After(*first) {
		t.Errorf("restart: startedAt %v → %v", first, s)
	}

	start := time.Now()
	do(ActionStop, ActionOptions{}, StateExited)
	// sleep ignores SIGTERM as PID 1, so stop waits for the 10 s timeout and kills it.
	t.Logf("stop took %v", time.Since(start).Round(time.Second))
	do(ActionStop, ActionOptions{}, StateExited) // no-op like nerdctl
	precondition(ActionKill, ActionOptions{})

	do(ActionStart, ActionOptions{}, StateRunning)
	do(ActionKill, ActionOptions{}, StateExited)
	if got := state(); got.ExitCode == nil || *got.ExitCode != 137 {
		t.Errorf("kill: exit code %v, want 137", got.ExitCode)
	}
	// The restart policy must not bring a killed container back (explicitly stopped).
	do(ActionStart, ActionOptions{}, StateRunning)

	precondition(ActionRemove, ActionOptions{})
	do(ActionRemove, ActionOptions{Force: true, Volumes: true}, "removed")
	if out, err := exec.Command("nerdctl", "volume", "inspect", volume).CombinedOutput(); err == nil {
		t.Errorf("anonymous volume %s still exists: %s", volume, out)
	}
	// The name and port are released: nerdctl can reuse them right away.
	nerdctl("run", "-d", "--name", name, "-p", "127.0.0.1:18081:80", "docker.io/library/alpine:3", "sleep", "1")

	if err := cc.Do(ctx, DefaultNamespace, id, ActionStart, ActionOptions{}); !errdefs.IsNotFound(err) {
		t.Errorf("removed container: err = %v, want NotFound", err)
	}
	if err := cc.Do(ctx, DefaultNamespace, "../etc", ActionStart, ActionOptions{}); !errdefs.IsInvalidArgument(err) {
		t.Errorf("invalid id: err = %v, want InvalidArgument", err)
	}
}
