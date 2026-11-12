// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package events

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func listEvents(t *testing.T, st *store.Store, p db.ListEventsParams) []db.Event {
	t.Helper()
	if p.Lim == 0 {
		p.Lim = 100
	}
	if p.Environment == "" {
		p.Environment = "local"
	}
	rows, err := st.Read().ListEvents(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRecorder(t *testing.T) {
	st := openStore(t)
	src := newSource(0)
	h := startHub(t, src)
	sess := src.next(t)

	var mu sync.Mutex
	alive := map[string]string{"default/c1": "web"}
	r := &Recorder{
		Environment: "local", Hub: h, Store: st, Retention: time.Hour,
		Names: func(_ context.Context, ns, id string) string {
			mu.Lock()
			defer mu.Unlock()
			return alive[ns+"/"+id]
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	now := time.Now()
	ev := func(topic, kind, action, ns, id string, attrs map[string]string) runtime.Event {
		return runtime.Event{Timestamp: now, Topic: topic, Kind: kind, Action: action, Namespace: ns, ID: id, Attributes: attrs}
	}
	// Wait until the recorder subscribed, so no event is missed.
	deadline := time.Now().Add(5 * time.Second)
	for h.subscribers() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("recorder did not subscribe")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitRows := func(n int) []db.Event {
		t.Helper()
		var rows []db.Event
		for deadline := time.Now().Add(5 * time.Second); len(rows) < n; rows = listEvents(t, st, db.ListEventsParams{}) {
			if time.Now().After(deadline) {
				t.Fatalf("recorded %d events, want %d", len(rows), n)
			}
			time.Sleep(20 * time.Millisecond)
		}
		return rows
	}
	sess.events <- ev("/containers/create", "container", "create", "default", "c1", nil)
	sess.events <- ev("/tasks/exit", "task", "exit", "default", "c1", map[string]string{"exitStatus": "137"})
	waitRows(2)
	mu.Lock()
	delete(alive, "default/c1") // gone before its delete event: the name comes from the cache
	mu.Unlock()
	sess.events <- ev("/containers/delete", "container", "delete", "default", "c1", nil)
	sess.events <- ev("/images/create", "image", "create", "k8s.io", "docker.io/library/alpine:3", nil)
	rows := waitRows(4)
	if rows[0].Kind != "image" || rows[3].Action != "create" {
		t.Errorf("not newest first: %+v", rows)
	}
	for _, row := range rows[1:] {
		if row.ObjectName != "web" {
			t.Errorf("container event without name: %+v", row)
		}
	}
	if rows[2].Attributes != `{"exitStatus":"137"}` || rows[2].Ts != now.UnixMilli() {
		t.Errorf("exit event %+v", rows[2])
	}

	// Filters and cursor.
	if got := listEvents(t, st, db.ListEventsParams{Namespace: "k8s.io"}); len(got) != 1 {
		t.Errorf("namespace filter: %d", len(got))
	}
	if got := listEvents(t, st, db.ListEventsParams{Kind: "task"}); len(got) != 1 {
		t.Errorf("kind filter: %d", len(got))
	}
	if got := listEvents(t, st, db.ListEventsParams{Object: "web"}); len(got) != 3 {
		t.Errorf("object filter by name: %d", len(got))
	}
	if got := listEvents(t, st, db.ListEventsParams{Before: rows[1].ID, Lim: 1}); len(got) != 1 || got[0].ID != rows[2].ID {
		t.Errorf("cursor: %+v", got)
	}
}

func TestRecorderCleanup(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	now := time.Now()
	for i, age := range []time.Duration{48 * time.Hour, 2 * time.Hour, time.Minute, 0} {
		if err := st.Write().InsertEvent(ctx, db.InsertEventParams{
			Environment: "local", Ts: now.Add(-age).UnixMilli(), Topic: "/t", Kind: "task", Action: "x", ObjectID: string(rune('a' + i)), Attributes: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}
	r := &Recorder{Environment: "local", Store: st, Retention: 24 * time.Hour, MaxEvents: 2,
		Logger: slog.Default(), now: func() time.Time { return now }}
	r.cleanup(ctx)
	rows := listEvents(t, st, db.ListEventsParams{})
	if len(rows) != 2 || rows[0].ObjectID != "d" || rows[1].ObjectID != "c" {
		t.Errorf("after cleanup: %+v", rows)
	}
}
