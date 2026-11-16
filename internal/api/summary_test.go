// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := writable()
	rt.summary = runtime.Summary{
		Containers:  runtime.ContainerCounts{Total: 3, Running: 2, Stopped: 1},
		Namespaces:  []runtime.NamespaceSummary{{Name: "default"}},
		Filesystems: []runtime.Filesystem{{Paths: []string{"/var/lib/containerd"}, Total: 100, Free: 40}},
	}
	rt.containers = []runtime.Container{
		{ID: "a", Compose: &runtime.ComposeRef{Project: "app", Service: "web"}},
		{ID: "b", Compose: &runtime.ComposeRef{Project: "manual", Service: "web"}},
	}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: jobs.NewManager(), Stacks: &stacks.Service{Dir: dir}}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	rec := send(t, r, http.MethodGet, "/api/v1/environments/local/summary?volumeSizes=true", "")
	var got SummaryResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	// One stack with files and one compose project known from labels.
	if rec.Code != http.StatusOK || got.Containers.Running != 2 || got.Stacks != 2 || len(got.Filesystems) != 1 || !rt.gotVolSizes {
		t.Fatalf("summary: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, "/api/v1/environments/local/summary?volumeSizes=x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad flag: %d", rec.Code)
	}
}

func TestTopBy(t *testing.T) {
	all := []TopContainer{{Name: "a", CPUPercent: 5}, {Name: "b", CPUPercent: 50}, {Name: "c", CPUPercent: 5},
		{Name: "d", CPUPercent: 1}, {Name: "e", CPUPercent: 9}, {Name: "f", CPUPercent: 0}}
	got := topBy(all, func(t TopContainer) float64 { return t.CPUPercent })
	var names []string
	for _, g := range got {
		names = append(names, g.Name)
	}
	if strings.Join(names, ",") != "b,e,a,c,d" {
		t.Errorf("top %v", names)
	}
	if len(all) != 6 || all[0].Name != "a" {
		t.Error("input changed")
	}
}

func TestTopStream(t *testing.T) {
	old := topInterval
	topInterval = 50 * time.Millisecond
	t.Cleanup(func() { topInterval = old })
	rt := &fakeRuntime{
		ns:           []string{"default"},
		containers:   []runtime.Container{{ID: "a1", Name: "web", State: runtime.StateRunning}, {ID: "s1", Name: "old", State: "exited"}},
		statsSamples: []runtime.StatsSample{{CPUPercent: 12.5, MemoryUsage: 64 << 20, MemoryPercent: 25}},
		statsHold:    true,
	}
	srv := httptest.NewServer(newRouter(rt))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/environments/local/stats/stream",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {srv.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m struct {
			Type string
			Data TopStats
		}
		_ = json.Unmarshal(b, &m)
		if m.Type != "data" || len(m.Data.CPU) == 0 {
			continue
		}
		if m.Data.Running != 1 || m.Data.CPU[0].Name != "web" || m.Data.CPU[0].CPUPercent != 12.5 ||
			m.Data.Memory[0].MemoryUsage != 64<<20 || m.Data.CPU[0].Namespace != "default" {
			t.Errorf("top %+v", m.Data)
		}
		return
	}
}
