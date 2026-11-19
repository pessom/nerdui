// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

func TestContainerNotifications(t *testing.T) {
	var mu sync.Mutex
	var got []notify.Event
	hook := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ev notify.Event
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &ev)
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}))
	t.Cleanup(hook.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg, _ := json.Marshal(notify.Config{URL: hook.URL})
	kinds, _ := json.Marshal([]string{notify.KindContainerDied, notify.KindContainerOOM})
	if _, err := st.Write().CreateNotificationChannel(ctx, db.CreateNotificationChannelParams{
		Name: "hook", Type: notify.TypeWebhook, Config: string(cfg), Events: string(kinds), Enabled: 1, Now: 1,
	}); err != nil {
		t.Fatal(err)
	}
	notifier := &notify.Service{Store: st.Write(), Host: "vps", Client: hook.Client()}
	go notifier.Run(ctx)

	src := &fakeSource{ch: make(chan runtime.Event, 16)}
	rt := &fakeRuntime{events: src, containers: []runtime.Container{
		{ID: "c1", Name: "web", Image: "nginx:1", State: "exited"},
		{ID: "c2", Name: "stopped", Image: "nginx:1", Labels: map[string]string{"containerd.io/restart.explicitly-stopped": "true"}},
	}}
	env := environment.Local(rt, "default")
	go env.Events.Run(ctx)
	w := &notify.Watcher{Env: env, Notifier: notifier, Delay: 10 * time.Millisecond}
	go w.Run(ctx)
	for !env.Events.Connected() {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the watcher subscribes

	exit := func(id, status string, attrs ...string) runtime.Event {
		a := map[string]string{"exitStatus": status}
		for i := 0; i+1 < len(attrs); i += 2 {
			a[attrs[i]] = attrs[i+1]
		}
		return runtime.Event{Namespace: "default", Topic: "/tasks/exit", Kind: "task", Action: "exit", ID: id, Attributes: a}
	}
	for _, ev := range []runtime.Event{
		exit("c1", "0"),                // clean exit
		exit("c2", "137"),              // stopped on purpose
		exit("gone", "137"),            // removed meanwhile (rm -f)
		exit("c1", "1", "execId", "x"), // an exec process
		exit("c1", "1"),                // a crash
		exit("c1", "1"),                // the same crash again: deduplicated
		{Namespace: "default", Topic: "/tasks/oom", Kind: "task", Action: "oom", ID: "c1"},
	} {
		src.ch <- ev
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	var titles []string
	for _, e := range got {
		titles = append(titles, e.Title)
		if e.Host != "vps" || e.Environment != "local" || e.Namespace != "default" || e.Severity != notify.SeverityError {
			t.Errorf("event %+v", e)
		}
	}
	slices.Sort(titles)
	if !slices.Equal(titles, []string{"Container web exited with code 1", "Container web ran out of memory"}) {
		t.Errorf("notifications %q", titles)
	}
}
