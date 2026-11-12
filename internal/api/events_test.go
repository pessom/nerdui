// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// fakeSource delivers events written to ch.
type fakeSource struct{ ch chan runtime.Event }

func (f *fakeSource) Subscribe(ctx context.Context) (<-chan runtime.Event, <-chan error, error) {
	if f.ch == nil {
		return nil, nil, runtime.ErrNotConnected
	}
	errc := make(chan error)
	go func() { <-ctx.Done(); close(errc) }()
	return f.ch, errc, nil
}

func startEventsServer(t *testing.T, src *fakeSource) (base string, env *environment.Environment) {
	t.Helper()
	env = environment.Local(&fakeRuntime{events: src}, "default")
	h := &Handler{Envs: environment.NewRegistry(env), Jobs: jobs.NewManager()}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil)) })
	srv := httptest.NewServer(r)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { env.Events.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; srv.Close() })
	return "ws" + strings.TrimPrefix(srv.URL, "http"), env
}

type msg struct {
	Type string
	Data runtime.Event
}

func readEvent(t *testing.T, c *websocket.Conn) msg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m msg
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func waitConnected(t *testing.T, env *environment.Environment) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !env.Events.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("hub not connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEventsStream(t *testing.T) {
	src := &fakeSource{ch: make(chan runtime.Event, 10)}
	base, env := startEventsServer(t, src)
	waitConnected(t, env)

	ctx := context.Background()
	c, _, err := websocket.Dial(ctx, base+"/api/v1/environments/local/events/stream?topics=/containers/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()

	if m := readEvent(t, c); m.Type != "data" || m.Data.Action != "connected" {
		t.Fatalf("first message: %+v", m)
	}
	src.ch <- runtime.Event{Topic: "/tasks/exit", Namespace: "default"}                    // topic filtered
	src.ch <- runtime.Event{Topic: "/containers/create", Namespace: "k8s.io", ID: "other"} // namespace filtered
	src.ch <- runtime.Event{Topic: "/containers/create", Namespace: "default", ID: "web", Kind: "container"}
	if m := readEvent(t, c); m.Data.ID != "web" || m.Data.Topic != "/containers/create" {
		t.Errorf("event: %+v", m)
	}
}

func TestEventsStreamAllNamespaces(t *testing.T) {
	src := &fakeSource{ch: make(chan runtime.Event, 10)}
	base, env := startEventsServer(t, src)
	waitConnected(t, env)
	c, _, err := websocket.Dial(context.Background(), base+"/api/v1/environments/local/events/stream?namespace=*", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	readEvent(t, c) // connection state
	src.ch <- runtime.Event{Topic: "/images/create", Namespace: "k8s.io", ID: "alpine"}
	if m := readEvent(t, c); m.Data.Namespace != "k8s.io" {
		t.Errorf("event from another namespace not delivered: %+v", m)
	}
}

func TestEventsStreamDisconnectedState(t *testing.T) {
	base, _ := startEventsServer(t, &fakeSource{}) // containerd unavailable
	c, _, err := websocket.Dial(context.Background(), base+"/api/v1/environments/local/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	if m := readEvent(t, c); m.Data.Action != "disconnected" {
		t.Errorf("first message: %+v", m)
	}
}

func TestEventsStreamBadParams(t *testing.T) {
	base, _ := startEventsServer(t, &fakeSource{ch: make(chan runtime.Event)})
	httpBase := "http" + strings.TrimPrefix(base, "ws")
	for _, q := range []string{"?namespace=../x", "?topics=containers"} {
		resp, err := http.Get(httpBase + "/api/v1/environments/local/events/stream" + q) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, resp.StatusCode)
		}
	}
}
