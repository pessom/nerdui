// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/pessom/nerdui/internal/runtime"
)

// fakeLogs serves per-container history; with Follow it also sends follow lines (which may
// repeat history, as the store filters by second) and then waits for ctx or release.
type fakeLogs struct {
	mu      sync.Mutex
	history map[string][]runtime.LogLine
	follow  map[string][]runtime.LogLine
	release chan struct{}
	opts    map[string][]runtime.LogOptions
}

func (f *fakeLogs) read(ctx context.Context, id string, o runtime.LogOptions, emit runtime.LogEmitter) error {
	f.mu.Lock()
	if f.opts == nil {
		f.opts = map[string][]runtime.LogOptions{}
	}
	f.opts[id] = append(f.opts[id], o)
	f.mu.Unlock()
	lines := f.history[id]
	if o.Follow {
		lines = f.follow[id]
	}
	for _, l := range lines {
		if err := emit(l); err != nil {
			return err
		}
	}
	if o.Follow && f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func at(sec int) time.Time { return time.Date(2027, 3, 1, 10, 0, sec, 0, time.UTC) }

func line(sec int, text string) runtime.LogLine {
	return runtime.LogLine{Stream: "stdout", Time: at(sec), Line: text}
}

func TestStreamStackLogs(t *testing.T) {
	containers := []stackContainer{{id: "w1", name: "app-web-1", service: "web"}, {id: "d1", name: "app-db-1", service: "db"}}
	f := &fakeLogs{
		history: map[string][]runtime.LogLine{
			"w1": {line(1, "web 1"), line(4, "web 4")},
			"d1": {line(2, "db 2"), line(3, "db 3")},
		},
		follow: map[string][]runtime.LogLine{
			"w1": {line(4, "web 4"), line(5, "web 5")}, // repeats the last history line
			"d1": {line(6, "db 6")},
		},
	}
	var mu sync.Mutex
	var got []string
	emit := func(l StackLogLine) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, l.Service+":"+l.Line)
		return nil
	}

	// History only: merged by time.
	if err := streamStackLogs(context.Background(), containers, runtime.LogOptions{Tail: 100}, f.read, emit); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"web:web 1", "db:db 2", "db:db 3", "web:web 4"}) {
		t.Errorf("history %v", got)
	}

	// Follow: then new lines of each container, without repeating the history.
	got = nil
	if err := streamStackLogs(context.Background(), containers, runtime.LogOptions{Tail: 100, Follow: true}, f.read, emit); err != nil {
		t.Fatal(err)
	}
	count := func(s string) (n int) {
		for _, g := range got {
			if g == s {
				n++
			}
		}
		return n
	}
	if len(got) != 6 || count("web:web 5") != 1 || count("db:db 6") != 1 || count("web:web 4") != 1 ||
		slices.Index(got, "web:web 4") != 3 {
		t.Errorf("follow %v", got)
	}
	// Followed from the last history line, without tail.
	o := f.opts["w1"][len(f.opts["w1"])-1]
	if !o.Follow || o.Tail != 0 || o.Since != at(4).Format(time.RFC3339Nano) {
		t.Errorf("follow options %+v", o)
	}

	// A client that goes away stops every reader.
	f.release = make(chan struct{})
	gone := errors.New("gone")
	n := 0
	err := streamStackLogs(context.Background(), containers, runtime.LogOptions{Tail: 100, Follow: true}, f.read, func(StackLogLine) error {
		mu.Lock()
		defer mu.Unlock()
		if n++; n > 4 {
			return gone
		}
		return nil
	})
	if !errors.Is(err, gone) {
		t.Errorf("client gone: %v", err)
	}

	// A failing reader is reported.
	boom := errors.New("boom")
	err = streamStackLogs(context.Background(), containers, runtime.LogOptions{Tail: 1}, func(context.Context, string, runtime.LogOptions, runtime.LogEmitter) error {
		return boom
	}, emit)
	if !errors.Is(err, boom) {
		t.Errorf("reader error: %v", err)
	}
}

func TestStackLogsStream(t *testing.T) {
	rt := &fakeRuntime{
		containers: []runtime.Container{
			{ID: "w1", Name: "app-web-1", Compose: &runtime.ComposeRef{Project: "app", Service: "web"}},
			{ID: "x1", Name: "other", Compose: &runtime.ComposeRef{Project: "other", Service: "web"}},
		},
		logs: []runtime.LogLine{line(1, "hello")},
	}
	srv := httptest.NewServer(newRouter(rt))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/environments/local/stacks/app/logs/stream?namespace=k8s.io&tail=all"
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Type string
		Data []StackLogLine
	}
	_ = json.Unmarshal(b, &m)
	if m.Type != "data" || len(m.Data) != 1 || m.Data[0].Service != "web" || m.Data[0].ContainerID != "w1" ||
		m.Data[0].Container != "app-web-1" || m.Data[0].Line != "hello" {
		t.Fatalf("message %s", b)
	}
	if end := readLogMsg(t, c); end.Type != "end" {
		t.Errorf("end %+v", end)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// Only the stack's container was read, with the tail capped and the namespace kept.
	if rt.gotNS != "k8s.io" || rt.gotLogs.Tail != maxStackTail || rt.gotLogs.Follow {
		t.Errorf("read ns %q options %+v", rt.gotNS, rt.gotLogs)
	}
}
