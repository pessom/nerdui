// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/runtime"
)

// scriptedSource fails the first `failures` subscriptions, then serves sessions whose
// event channel the test controls via `sessions`.
type scriptedSource struct {
	mu       sync.Mutex
	failures int
	calls    int
	sessions chan *session
}

type session struct {
	events chan runtime.Event
	errs   chan error
}

func newSource(failures int) *scriptedSource {
	return &scriptedSource{failures: failures, sessions: make(chan *session, 10)}
}

func (s *scriptedSource) Subscribe(_ context.Context) (<-chan runtime.Event, <-chan error, error) {
	s.mu.Lock()
	s.calls++
	fail := s.calls <= s.failures
	s.mu.Unlock()
	if fail {
		return nil, nil, runtime.ErrNotConnected
	}
	sess := &session{events: make(chan runtime.Event, 100), errs: make(chan error, 1)}
	s.sessions <- sess
	return sess.events, sess.errs, nil
}

func (s *scriptedSource) next(t *testing.T) *session {
	t.Helper()
	select {
	case sess := <-s.sessions:
		return sess
	case <-time.After(5 * time.Second):
		t.Fatal("no subscription")
		return nil
	}
}

func recv(t *testing.T, sub *Subscription) runtime.Event {
	t.Helper()
	select {
	case ev, ok := <-sub.C:
		if !ok {
			t.Fatal("subscription closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
		return runtime.Event{}
	}
}

func startHub(t *testing.T, src runtime.EventSource) *Hub {
	t.Helper()
	h := newTestHub(src)
	run(t, h)
	return h
}

func newTestHub(src runtime.EventSource) *Hub {
	h := NewHub(src, nil)
	h.MinBackoff, h.MaxBackoff = 10*time.Millisecond, 40*time.Millisecond
	return h
}

func run(t *testing.T, h *Hub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestFilter(t *testing.T) {
	ev := runtime.Event{Namespace: "default", Topic: "/tasks/exit"}
	for name, tt := range map[string]struct {
		f    Filter
		want bool
	}{
		"empty":          {Filter{}, true},
		"namespace":      {Filter{Namespace: "default"}, true},
		"other ns":       {Filter{Namespace: "k8s.io"}, false},
		"topic prefix":   {Filter{Topics: []string{"/containers/", "/tasks/"}}, true},
		"exact topic":    {Filter{Topics: []string{"/tasks/exit"}}, true},
		"other topic":    {Filter{Topics: []string{"/images/"}}, false},
		"synthetic only": {Filter{Namespace: "x", Topics: []string{"/none"}}, false},
	} {
		if got := tt.f.Match(ev); got != tt.want {
			t.Errorf("%s: Match = %v", name, got)
		}
	}
	syn := runtime.Event{Kind: KindNerdui, Topic: TopicConnected}
	if !(Filter{Namespace: "x", Topics: []string{"/none"}}).Match(syn) {
		t.Error("synthetic events must always pass")
	}
}

func TestHubDeliversAndReconnects(t *testing.T) {
	src := newSource(2) // containerd down for the first two attempts
	h := newTestHub(src)
	sub := h.Subscribe(Filter{Namespace: "default"}, 10) // before Run: deterministic order
	defer sub.Close()
	run(t, h)
	if ev := recv(t, sub); ev.Action != ActionDisconnected {
		t.Fatalf("initial state: %+v", ev)
	}

	sess := src.next(t)
	if ev := recv(t, sub); ev.Action != ActionConnected {
		t.Fatalf("after connect: %+v", ev)
	}
	sess.events <- runtime.Event{Namespace: "k8s.io", Topic: "/containers/create"}
	sess.events <- runtime.Event{Namespace: "default", Topic: "/containers/create", ID: "web"}
	if ev := recv(t, sub); ev.ID != "web" {
		t.Errorf("event: %+v", ev)
	}

	// containerd restarts: the subscription breaks and is re-established.
	sess.errs <- errors.New("transport is closing")
	close(sess.events)
	if ev := recv(t, sub); ev.Action != ActionDisconnected {
		t.Fatalf("after break: %+v", ev)
	}
	sess2 := src.next(t)
	if ev := recv(t, sub); ev.Action != ActionConnected {
		t.Fatalf("after reconnect: %+v", ev)
	}
	sess2.events <- runtime.Event{Namespace: "default", Topic: "/tasks/start", ID: "web"}
	if ev := recv(t, sub); ev.Topic != "/tasks/start" {
		t.Errorf("event after reconnect: %+v", ev)
	}
	if !h.Connected() {
		t.Error("Connected() = false")
	}
}

func TestSlowSubscriberDropped(t *testing.T) {
	src := newSource(0)
	h := startHub(t, src)
	sess := src.next(t)

	slow := h.Subscribe(Filter{}, 2)
	fast := h.Subscribe(Filter{}, 100)
	defer fast.Close()

	start := time.Now()
	for i := 0; i < 50; i++ {
		sess.events <- runtime.Event{Topic: "/containers/update"}
	}
	got := 0
	for got < 50 {
		ev := recv(t, fast)
		if ev.Kind != KindNerdui {
			got++
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a slow subscriber delayed others")
	}
	for range slow.C { // drain until closed
	}
	if !slow.Overflowed() {
		t.Error("slow subscriber not marked as overflowed")
	}
}

func TestRunClosesSubscriptions(t *testing.T) {
	src := newSource(0)
	h := NewHub(src, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()
	src.next(t)
	sub := h.Subscribe(Filter{}, 10)
	cancel()
	<-done
	for range sub.C {
	}
	if sub.Overflowed() {
		t.Error("shutdown reported as overflow")
	}
	sub.Close() // idempotent after hub shutdown
}

// TestStateAnnouncedOnce subscribes while the hub connects: every subscriber must see the
// "connected" state once, as its initial event or as the announcement, not both.
func TestStateAnnouncedOnce(t *testing.T) {
	for range 20 {
		src := newSource(0)
		h := newTestHub(src)
		const n = 50
		subs := make(chan *Subscription, n)
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() { subs <- h.Subscribe(Filter{}, 0) })
		}
		run(t, h)
		sess := src.next(t)
		wg.Wait()
		close(subs)
		sess.events <- runtime.Event{Topic: "/containers/create", ID: "marker"}
		for sub := range subs {
			connected := 0
			for {
				ev := recv(t, sub)
				if ev.ID == "marker" {
					break
				}
				if ev.Action == ActionConnected {
					connected++
				}
			}
			if connected != 1 {
				t.Fatalf("subscriber saw %d connected events", connected)
			}
		}
	}
}
