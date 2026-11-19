// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

// A crash is reported; stopping, killing and removing containers are not.
func TestIntegrationWatcher(t *testing.T) {
	var mu sync.Mutex
	var titles []string
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ev Event
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &ev)
		mu.Lock()
		titles = append(titles, ev.Title)
		mu.Unlock()
	}))
	t.Cleanup(hook.Close)
	cfg, _ := json.Marshal(Config{URL: hook.URL})
	kinds, _ := json.Marshal(Kinds)
	notifier := &Service{Client: hook.Client(), Store: &fakeStore{rows: []db.NotificationChannel{{
		ID: 1, Name: "hook", Type: TypeWebhook, Config: string(cfg), Events: string(kinds), Enabled: 1,
	}}}}

	ncfg, _, err := runtime.LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	c := runtime.New(runtime.DefaultAddress, runtime.DefaultNamespace, runtime.WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })
	env := environment.Local(c, runtime.DefaultNamespace)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	go env.Events.Run(ctx)
	go notifier.Run(ctx)
	go (&Watcher{Env: env, Notifier: notifier}).Run(ctx)
	for !env.Events.Connected() {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	suffix := strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
	crash, stopped, removed := "it-crash-"+suffix, "it-stop-"+suffix, "it-rm-"+suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", crash, stopped, removed).Run() })
	nerdctl := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("nerdctl", args...).Output()
		if err != nil {
			t.Fatalf("nerdctl %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	nerdctl("run", "-d", "--name", crash, "docker.io/library/alpine:3", "sh", "-c", "sleep 1; exit 3")
	stoppedID := nerdctl("run", "-d", "--name", stopped, "docker.io/library/alpine:3", "sleep", "600")
	nerdctl("run", "-d", "--name", removed, "docker.io/library/alpine:3", "sleep", "600")
	if err := c.Containers().Do(ctx, runtime.DefaultNamespace, stoppedID, runtime.ActionStop, runtime.ActionOptions{}); err != nil {
		t.Fatal(err)
	}
	nerdctl("rm", "-f", removed)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(titles)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(5 * time.Second) // let the stop and removal be judged too
	mu.Lock()
	defer mu.Unlock()
	if len(titles) != 1 || !strings.Contains(titles[0], crash+" exited with code 3") {
		t.Errorf("notifications %q", titles)
	}
}
