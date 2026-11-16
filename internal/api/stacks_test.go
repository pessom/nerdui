// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

func TestStacks(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"app", "broken"} {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rt := writable()
	rt.services = map[string][]string{"app": {"web", "db"}}
	rt.containers = []runtime.Container{
		{ID: "w1", Name: "app-web-1", State: runtime.StateRunning, Compose: &runtime.ComposeRef{Project: "app", Service: "web"}},
		{ID: "d1", Name: "app-db-1", State: "exited", Compose: &runtime.ComposeRef{Project: "app", Service: "db"}},
		{ID: "m1", Name: "manual-web-1", State: runtime.StateRunning, Compose: &runtime.ComposeRef{Project: "manual", Service: "web"}},
	}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: jobs.NewManager(),
			Stacks: &stacks.Service{Dir: dir},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})

	rec := send(t, r, http.MethodGet, "/api/v1/environments/local/stacks?namespace=k8s.io", "")
	var list struct {
		Namespace string
		Stacks    []stacks.Stack
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || list.Namespace != "k8s.io" || len(list.Stacks) != 3 || rt.gotNS != "k8s.io" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	got := map[string]stacks.Stack{}
	for _, s := range list.Stacks {
		got[s.Name] = s
	}
	if s := got["app"]; s.Status != stacks.StatusPartial || s.Mode != stacks.ModeManaged || s.Running != 1 || len(s.Services) != 2 {
		t.Errorf("app %+v", s)
	}
	if s := got["broken"]; s.Status != stacks.StatusUnknown || s.Error == "" {
		t.Errorf("broken %+v", s)
	}
	if s := got["manual"]; s.Mode != stacks.ModeUnmanaged || s.Running != 1 {
		t.Errorf("manual %+v", s)
	}

	rec = send(t, r, http.MethodGet, "/api/v1/environments/local/stacks/app", "")
	var one stacks.Stack
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if rec.Code != http.StatusOK || one.Name != "app" || one.Services[0].Containers[0].ID != "w1" {
		t.Errorf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, "/api/v1/environments/local/stacks/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing: %d", rec.Code)
	}
}
