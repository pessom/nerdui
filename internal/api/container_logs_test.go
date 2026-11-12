// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
)

var sampleLogs = []runtime.LogLine{
	{Stream: "stdout", Time: time.Date(2027, 3, 1, 10, 0, 0, 5, time.UTC), Line: "\x1b[32mready\x1b[0m"},
	{Stream: "stderr", Time: time.Date(2027, 3, 1, 10, 0, 1, 0, time.UTC), Line: "warning"},
	{Stream: "stdout", Line: "no time"},
}

func TestContainerLogsDownload(t *testing.T) {
	rt := &fakeRuntime{logs: sampleLogs}
	rec := send(t, newRouter(rt), http.MethodGet,
		"/api/v1/environments/local/containers/0123456789abcdef/logs?namespace=k8s.io&tail=100&since=10m&until=2027-03-01T11:00:00Z&timestamps=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := "2027-03-01T10:00:00.000000005Z \x1b[32mready\x1b[0m\n2027-03-01T10:00:01Z warning\nno time\n"
	if rec.Body.String() != want {
		t.Errorf("body %q", rec.Body)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="0123456789ab-logs.txt"` {
		t.Errorf("Content-Disposition %q", cd)
	}
	if rt.gotNS != "k8s.io" || rt.gotLogs != (runtime.LogOptions{Tail: 100, Since: "10m", Until: "2027-03-01T11:00:00Z"}) {
		t.Errorf("ns %q opts %+v", rt.gotNS, rt.gotLogs)
	}

	rec = send(t, newRouter(rt), http.MethodGet, "/api/v1/environments/local/containers/abc/logs", "")
	if !strings.HasPrefix(rec.Body.String(), "\x1b[32mready") {
		t.Errorf("without timestamps: %q", rec.Body)
	}

	for path, code := range map[string]int{
		"/api/v1/environments/local/containers/abc/logs?tail=x":       http.StatusBadRequest,
		"/api/v1/environments/local/containers/abc/logs?follow=true":  http.StatusBadRequest,
		"/api/v1/environments/local/containers/abc/logs?timestamps=x": http.StatusBadRequest,
	} {
		if rec := send(t, newRouter(rt), http.MethodGet, path, ""); rec.Code != code {
			t.Errorf("%s: %d, want %d", path, rec.Code, code)
		}
	}
	missing := &fakeRuntime{logErr: fmt.Errorf("%w: no such container: abc", errdefs.ErrNotFound)}
	if rec := send(t, newRouter(missing), http.MethodGet, "/api/v1/environments/local/containers/abc/logs", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing container: %d", rec.Code)
	}
}

func dialLogs(t *testing.T, rt *fakeRuntime, query string) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(newRouter(rt))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/environments/local/containers/abc/logs/stream" + query
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

type logMsg struct {
	Type  string
	Data  []runtime.LogLine
	Error struct{ Code, Message string }
}

func readLogMsg(t *testing.T, c *websocket.Conn) logMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m logMsg
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestContainerLogsStream(t *testing.T) {
	rt := &fakeRuntime{logs: sampleLogs}
	c := dialLogs(t, rt, "?tail=all")
	m := readLogMsg(t, c)
	if m.Type != "data" || len(m.Data) != 3 || m.Data[1].Stream != "stderr" || m.Data[0].Line != sampleLogs[0].Line {
		t.Fatalf("first message: %+v", m)
	}
	if !m.Data[0].Time.Equal(sampleLogs[0].Time) || !m.Data[2].Time.IsZero() {
		t.Errorf("times: %+v", m.Data)
	}
	if m := readLogMsg(t, c); m.Type != "end" {
		t.Fatalf("want end, got %+v", m)
	}
}

func TestContainerLogsStreamFollowAndDisconnect(t *testing.T) {
	rt := &fakeRuntime{logs: sampleLogs[:1], logFollow: true}
	c := dialLogs(t, rt, "?follow=1&tail=10&since=2027-03-01T10:00:00Z")
	if m := readLogMsg(t, c); m.Type != "data" || len(m.Data) != 1 {
		t.Fatalf("%+v", m)
	}
	rt.mu.Lock()
	got := rt.gotLogs
	rt.mu.Unlock()
	if got != (runtime.LogOptions{Follow: true, Tail: 10, Since: "2027-03-01T10:00:00Z"}) {
		t.Errorf("opts %+v", got)
	}
	// Closing the socket must end the follow (the fake blocks until its context ends).
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestContainerLogsStreamErrors(t *testing.T) {
	rt := &fakeRuntime{logErr: fmt.Errorf("%w: no such container: abc", errdefs.ErrNotFound)}
	c := dialLogs(t, rt, "")
	if m := readLogMsg(t, c); m.Type != "error" || m.Error.Code != "not_found" {
		t.Fatalf("%+v", m)
	}
	if m := readLogMsg(t, c); m.Type != "end" {
		t.Fatalf("%+v", m)
	}
	if rec := send(t, newRouter(rt), http.MethodGet, "/api/v1/environments/local/containers/abc/logs/stream?tail=-1", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid tail before upgrade: %d", rec.Code)
	}
}
