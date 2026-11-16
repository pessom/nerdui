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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
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

func TestAdoptStack(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	stacksDir, scan := t.TempDir(), t.TempDir()
	media := filepath.Join(scan, "media")
	if err := os.MkdirAll(media, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := writable()
	rt.services = map[string][]string{"media": {"web"}}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: jobs.NewManager(),
			Stacks: &stacks.Service{Dir: stacksDir, Registry: st.Read(), Writer: st.Write(), ScanDirs: []string{scan}},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local"

	rec := send(t, r, http.MethodGet, base+"/stack-candidates", "")
	var found struct {
		ScanDirs   []string
		Candidates []stacks.Candidate
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &found)
	if rec.Code != http.StatusOK || len(found.Candidates) != 1 || found.Candidates[0].Path != media {
		t.Fatalf("candidates: %d %s", rec.Code, rec.Body)
	}

	rec = send(t, r, http.MethodPost, base+"/stacks/adopt", `{"name":"media","path":"`+media+`","mode":"in-place"}`)
	var adopted stacks.Stack
	_ = json.Unmarshal(rec.Body.Bytes(), &adopted)
	if rec.Code != http.StatusCreated || adopted.Mode != stacks.ModeExternal || adopted.Status != stacks.StatusStopped {
		t.Fatalf("adopt: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, base+"/stacks/adopt", `{"name":"media","path":"`+media+`","mode":"in-place"}`); rec.Code != http.StatusConflict {
		t.Errorf("adopt twice: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, base+"/stacks/adopt", `{"name":"x","path":"relative","mode":"copy"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("relative path: %d %s", rec.Code, rec.Body)
	}
	// Adoption does not need a compatible nerdctl: it touches files and the database only.
	if rec := send(t, r, http.MethodDelete, base+"/stacks/media/registration", ""); rec.Code != http.StatusNoContent {
		t.Errorf("forget: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodDelete, base+"/stacks/media/registration", ""); rec.Code != http.StatusNotFound {
		t.Errorf("forget again: %d %s", rec.Code, rec.Body)
	}
}

func TestStackOperations(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := writable()
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: m,
			Stacks: &stacks.Service{Dir: dir},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local/stacks"
	wait := func(rec *httptest.ResponseRecorder) jobs.Job {
		t.Helper()
		var resp struct{ Job jobs.Job }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		deadline := time.Now().Add(5 * time.Second)
		for j, _ := m.Get(resp.Job.ID); ; j, _ = m.Get(resp.Job.ID) {
			if j.Done() {
				return j
			}
			if time.Now().After(deadline) {
				t.Fatalf("job %s not done", resp.Job.ID)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	rec := send(t, r, http.MethodPost, base+"/app/up?namespace=k8s.io", `{"pull":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("up: %d %s", rec.Code, rec.Body)
	}
	if j := wait(rec); j.Status != jobs.StatusSucceeded || j.Title != "Deploy stack app" || j.Namespace != "k8s.io" || j.Action != string(authz.StacksDeploy) {
		t.Errorf("up job %+v", j)
	}
	if rt.composeRuns[0] != "up app "+filepath.Join(dir, "app") || !rt.composeOpts[0].Pull {
		t.Errorf("run %v %+v", rt.composeRuns, rt.composeOpts)
	}
	// A stop without a body.
	if rec := send(t, r, http.MethodPost, base+"/app/stop", ""); rec.Code != http.StatusAccepted {
		t.Errorf("stop: %d %s", rec.Code, rec.Body)
	} else {
		wait(rec)
	}

	// down -v needs the stack name as confirmation.
	if rec := send(t, r, http.MethodPost, base+"/app/down", `{"volumes":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("down -v unconfirmed: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, r, http.MethodPost, base+"/app/down", `{"volumes":true,"confirm":"app"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("down -v: %d %s", rec.Code, rec.Body)
	}
	if j := wait(rec); j.Title != "Down (removing volumes) stack app" || !rt.composeOpts[2].Volumes {
		t.Errorf("down -v job %+v %+v", j, rt.composeOpts)
	}

	for path, code := range map[string]int{
		base + "/app/explode":  http.StatusNotFound, // unknown operation
		base + "/ghost/up":     http.StatusNotFound, // no such stack
		base + "/app/restart?": http.StatusAccepted, // ok
		base + "/app/pull":     http.StatusAccepted, // ok
		base + "/app/start":    http.StatusAccepted, // ok
	} {
		rec := send(t, r, http.MethodPost, path, "")
		if rec.Code != code {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if rec.Code == http.StatusAccepted {
			wait(rec)
		}
	}
	if rec := send(t, r, http.MethodPost, base+"/app/start", `{"pull":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("pull option on start: %d %s", rec.Code, rec.Body)
	}

	// One job per stack at a time.
	rt.mu.Lock()
	rt.composeHold = make(chan struct{})
	rt.mu.Unlock()
	first := send(t, r, http.MethodPost, base+"/app/restart", "")
	if rec := send(t, r, http.MethodPost, base+"/app/stop", ""); rec.Code != http.StatusConflict {
		t.Errorf("second job: %d %s", rec.Code, rec.Body)
	}
	close(rt.composeHold)
	wait(first)

	// Operations need a writable nerdctl.
	ro := chi.NewRouter()
	ro.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")), Jobs: m, Stacks: &stacks.Service{Dir: dir}}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	if rec := send(t, ro, http.MethodPost, base+"/app/up", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("read-only: %d", rec.Code)
	}
}
