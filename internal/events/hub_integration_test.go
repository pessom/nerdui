// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package events

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
)

// Requires root, containerd and nerdctl (CI integration job). With
// NERDUI_TEST_RESTART_CONTAINERD=1 the test also restarts containerd via systemd.
func TestIntegrationEventsFromNerdctl(t *testing.T) {
	addr := os.Getenv("NERDUI_TEST_ADDRESS")
	if addr == "" {
		addr = runtime.DefaultAddress
	}
	rt := runtime.New(addr, runtime.DefaultNamespace)
	t.Cleanup(func() { _ = rt.Close() })

	h := NewHub(rt.Events(), nil)
	h.MinBackoff, h.MaxBackoff = 200*time.Millisecond, 2*time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	sub := h.Subscribe(Filter{Namespace: runtime.DefaultNamespace, Topics: []string{"/containers/create"}}, 100)
	defer sub.Close()
	waitFor(t, sub, 15*time.Second, func(ev runtime.Event) bool { return ev.Action == ActionConnected })

	// NFR-003: a change made with the nerdctl CLI reaches subscribers within 2 seconds.
	name := runContainer(t, "a")
	ev := waitFor(t, sub, 30*time.Second, func(ev runtime.Event) bool { return ev.Topic == "/containers/create" })
	if lag := time.Since(ev.Timestamp); lag > 2*time.Second {
		t.Errorf("event for %s delivered %v after it happened (NFR-003: 2s)", name, lag)
	}
	t.Logf("container %s: event %s id=%s lag=%v", name, ev.Topic, ev.ID, time.Since(ev.Timestamp))

	if os.Getenv("NERDUI_TEST_RESTART_CONTAINERD") != "1" {
		t.Log("skipping containerd restart (set NERDUI_TEST_RESTART_CONTAINERD=1)")
		return
	}
	if out, err := exec.Command("systemctl", "restart", "containerd").CombinedOutput(); err != nil {
		t.Fatalf("restart containerd: %v: %s", err, out)
	}
	waitFor(t, sub, 30*time.Second, func(ev runtime.Event) bool { return ev.Action == ActionDisconnected })
	waitFor(t, sub, 60*time.Second, func(ev runtime.Event) bool { return ev.Action == ActionConnected })
	name = runContainer(t, "b")
	waitFor(t, sub, 30*time.Second, func(ev runtime.Event) bool { return ev.Topic == "/containers/create" })
	t.Logf("events delivered again after containerd restart (%s)", name)
}

func runContainer(t *testing.T, suffix string) string {
	t.Helper()
	name := fmt.Sprintf("nerdui-it-events-%d-%s", time.Now().UnixNano(), suffix)
	out, err := exec.Command("nerdctl", "run", "--rm", "--name", name, "docker.io/library/alpine:3", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	return name
}

func waitFor(t *testing.T, sub *Subscription, timeout time.Duration, match func(runtime.Event) bool) runtime.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-sub.C:
			if !ok {
				t.Fatal("subscription closed")
			}
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("expected event not received")
			return runtime.Event{}
		}
	}
}
