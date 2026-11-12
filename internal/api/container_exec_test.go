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
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

func execServer(t *testing.T, f *fakeRuntime, disabled bool) string {
	t.Helper()
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(f, "default")), Jobs: jobs.NewManager(), ExecDisabled: disabled}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1/environments/local/containers/abc/exec"
}

func TestContainerExec(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
	url := execServer(t, f, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"?cmd=/bin/sh&cmd=-l&user=1000&cols=120&rows=30", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()

	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(b) != "echo: ls\r\n" {
		t.Fatalf("output %v %q %v", typ, b, err)
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("exit 3\n"))
	var msgs []map[string]any
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (%v)", err, msgs)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		msgs = append(msgs, m)
		if m["type"] == "end" {
			break
		}
	}
	if msgs[0]["data"].(map[string]any)["exitCode"].(float64) != 3 {
		t.Errorf("exit message %v", msgs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.gotExec.Cmd, " ") != "/bin/sh -l" || f.gotExec.User != "1000" || f.gotExec.Cols != 120 || f.gotExec.Rows != 30 {
		t.Errorf("exec options %+v", f.gotExec)
	}
	if len(f.gotSizes) != 1 || f.gotSizes[0] != (runtime.TermSize{Cols: 100, Rows: 40}) {
		t.Errorf("sizes %+v", f.gotSizes)
	}
}

func TestContainerExecErrors(t *testing.T) {
	// Disabled on the server: refused before the upgrade.
	resp, err := http.Get(execServer(t, &fakeRuntime{}, true)) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("disabled: %d", resp.StatusCode)
	}

	f := &fakeRuntime{execErr: fmt.Errorf("%w: container is not running", errdefs.ErrFailedPrecondition)}
	url := execServer(t, f, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	_, b, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(b), `"type":"error"`) || !strings.Contains(string(b), "not running") {
		t.Errorf("not running: %s %v", b, err)
	}

	if rec := send(t, newRouter(&fakeRuntime{}), http.MethodGet, "/api/v1/environments/local/containers/abc/exec?cols=x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad cols: %d", rec.Code)
	}
}
