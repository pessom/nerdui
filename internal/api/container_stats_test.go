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

func statsStream(t *testing.T, f *fakeRuntime, query string) []map[string]any {
	t.Helper()
	srv := httptest.NewServer(newRouter(f))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/environments/local/containers/abc/stats/stream"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	var msgs []map[string]any
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (after %v)", err, msgs)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		msgs = append(msgs, m)
		if m["type"] == "end" {
			return msgs
		}
	}
}

func TestContainerStatsStream(t *testing.T) {
	f := &fakeRuntime{statsSamples: []runtime.StatsSample{
		{CPUPercent: 12.5, MemoryUsage: 100, MemoryLimit: 1000},
		{CPUPercent: 50, MemoryUsage: 200, MemoryLimit: 1000, PIDs: 3},
	}}
	msgs := statsStream(t, f, "?interval=5s")
	if len(msgs) != 3 || msgs[0]["type"] != "data" || msgs[1]["data"].(map[string]any)["pids"].(float64) != 3 {
		t.Fatalf("messages %v", msgs)
	}
	if f.gotInterval != 5*time.Second {
		t.Errorf("interval %v", f.gotInterval)
	}

	f = &fakeRuntime{statsErr: fmt.Errorf("%w: container is not running", errdefs.ErrFailedPrecondition)}
	msgs = statsStream(t, f, "")
	if len(msgs) != 2 || msgs[0]["type"] != "error" || f.gotInterval != 2*time.Second {
		t.Errorf("not running: %v (interval %v)", msgs, f.gotInterval)
	}

	rec := send(t, newRouter(&fakeRuntime{}), http.MethodGet, "/api/v1/environments/local/containers/abc/stats/stream?interval=100ms", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("too short interval: %d", rec.Code)
	}
}
