// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/updates"
)

type fakeStackDefs []stacks.Definition

func (f fakeStackDefs) Definitions(context.Context) ([]stacks.Definition, error) { return f, nil }

func TestAutoUpdates(t *testing.T) {
	ctx := context.Background()
	on := map[string]string{updates.Label: "true"}
	rt := &fakeRuntime{
		ns: []string{"default"},
		images: []runtime.Image{
			{Name: "docker.io/library/nginx:1", Digest: "sha256:old"},
			{Name: "docker.io/library/redis:7", Digest: "sha256:same"},
		},
		containers: []runtime.Container{
			{ID: "1", Name: "web", Image: "docker.io/library/nginx:1", Labels: on},
			{ID: "2", Name: "plain", Image: "docker.io/library/nginx:1"},
			{ID: "3", Name: "app-web-1", Image: "docker.io/library/nginx:1", Labels: on,
				Compose: &runtime.ComposeRef{Project: "app", Service: "web"}},
			{ID: "4", Name: "app-db-1", Image: "docker.io/library/nginx:1",
				Compose: &runtime.ComposeRef{Project: "app", Service: "db"}},
			{ID: "5", Name: "cache", Image: "docker.io/library/redis:7", Labels: on},
			{ID: "6", Name: "orphan-x-1", Image: "docker.io/library/nginx:1", Labels: on,
				Compose: &runtime.ComposeRef{Project: "orphan", Service: "x"}},
		},
		remote: map[string]string{
			"docker.io/library/nginx:1": "sha256:new",
			"docker.io/library/redis:7": "sha256:same",
		},
		spec:     runtime.ContainerSpec{ID: "1", Spec: runtime.CreateSpec{Image: "docker.io/library/nginx:1", Name: "web"}},
		createID: "new1",
	}
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	env := environment.Local(rt, "default")
	envs := environment.NewRegistry(env)
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	checker := &updates.Checker{Environments: envs, Store: st.Write()}
	u := &updates.Updater{
		Store: st.Write(), Jobs: m,
		Stacks: fakeStackDefs{{Name: "app", Dir: "/srv/app", File: "compose.yaml"}},
	}
	if _, err := checker.Check(ctx, env, ""); err != nil {
		t.Fatal(err)
	}

	plan, err := u.Plan(ctx, env, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].Name != "web" || plan[1].Name != "app" || !slices.Equal(plan[1].Services, []string{"web"}) || plan[2].Name != "orphan" {
		t.Fatalf("plan %+v", plan)
	}

	if err := u.Run(ctx, env); err != nil {
		t.Fatal(err)
	}
	if len(rt.recreated) != 1 || !rt.recreatePull || rt.recreated[0].Name != "web" {
		t.Errorf("recreated %+v pull %v", rt.recreated, rt.recreatePull)
	}
	if !slices.Equal(rt.composeRuns, []string{"pull app /srv/app", "up app /srv/app"}) ||
		!slices.Equal(rt.composeOpts[1].Services, []string{"web"}) {
		t.Errorf("compose %v %+v", rt.composeRuns, rt.composeOpts)
	}
	log, err := u.Log(ctx, "local", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, e := range log {
		status[e.Kind+"/"+e.Name] = e.Status
		if e.Status == updates.StatusUpdated && e.JobID == "" {
			t.Errorf("no job id %+v", e)
		}
	}
	want := map[string]string{"container/web": "updated", "stack/app": "updated", "stack/orphan": "skipped"}
	if len(log) != 3 || status["container/web"] != want["container/web"] || status["stack/app"] != want["stack/app"] || status["stack/orphan"] != want["stack/orphan"] {
		t.Errorf("log %+v", log)
	}

	// Settings the form cannot express: skipped, and a repeated skip is logged once.
	rt.spec.Unsupported = []string{"tmpfs"}
	for range 2 {
		if err := u.Run(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	if len(rt.recreated) != 1 {
		t.Errorf("recreated a container with unsupported settings: %d", len(rt.recreated))
	}
	log, _ = u.Log(ctx, "local", "default", 0)
	skips := 0
	for _, e := range log {
		if e.Kind == updates.KindContainer && e.Status == updates.StatusSkipped {
			skips++
		}
	}
	if skips != 1 {
		t.Errorf("container skips %d: %+v", skips, log)
	}

	// The log over the API.
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		(&Handler{Envs: envs, Jobs: m, AutoUpdates: u}).Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	var body struct {
		Entries []updates.Entry `json:"entries"`
	}
	if rec := do(t, r, "/api/v1/environments/local/auto-updates?namespace=default", &body); rec.Code != http.StatusOK || len(body.Entries) != len(log) {
		t.Errorf("api: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments/local/auto-updates", nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("disabled: %d", rec.Code)
	}
}

func TestAutoEnabled(t *testing.T) {
	for v, want := range map[string]bool{"true": true, "TRUE": true, "1": true, "yes": true, "on": true, "false": false, "": false, "0": false} {
		if got := updates.AutoEnabled(map[string]string{updates.Label: v}); got != want {
			t.Errorf("%q: %v", v, got)
		}
	}
}

func TestAutoUpdateGuards(t *testing.T) {
	ctx := context.Background()
	on := map[string]string{updates.Label: "true"}
	rt := &fakeRuntime{
		ns:     []string{"default"},
		images: []runtime.Image{{Name: "docker.io/library/nginx:1", Digest: "sha256:old"}},
		containers: []runtime.Container{
			{ID: "1", Name: "a", Image: "docker.io/library/nginx:1", Labels: on},
			{ID: "2", Name: "b", Image: "docker.io/library/nginx:1", Labels: on},
			{ID: "3", Name: "c", Image: "docker.io/library/nginx:1", Labels: on},
		},
		remote: map[string]string{"docker.io/library/nginx:1": "sha256:new"},
		spec:   runtime.ContainerSpec{Spec: runtime.CreateSpec{Image: "docker.io/library/nginx:1"}},
	}
	recreated := func() int {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return len(rt.recreated)
	}
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	env := environment.Local(rt, "default")
	envs := environment.NewRegistry(env)
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	t0 := time.Now()
	now := t0
	clock := func() time.Time { return now }
	checker := &updates.Checker{Environments: envs, Store: st.Write(), Now: clock}
	u := &updates.Updater{Store: st.Write(), Jobs: m, Now: clock}
	if _, err := checker.Check(ctx, env, ""); err != nil {
		t.Fatal(err)
	}

	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		(&Handler{Envs: envs, Jobs: m, AutoUpdates: u}).Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	var set updates.AutoSettings
	if rec := do(t, r, "/api/v1/environments/local/settings/auto-updates", &set); rec.Code != http.StatusOK || set != (updates.AutoSettings{MaxBatch: 5}) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPut, "/api/v1/environments/local/settings/auto-updates", `{"delaySeconds":60,"maxBatch":0}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid delay: %d", rec.Code)
	}

	// A digest seen less than the delay ago is not applied.
	if rec := send(t, r, http.MethodPut, "/api/v1/environments/local/settings/auto-updates", `{"delaySeconds":3600,"maxBatch":0}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	now = t0.Add(30 * time.Minute)
	if err := u.Run(ctx, env); err != nil || recreated() != 0 {
		t.Fatalf("before the delay: %v, %d recreated", err, recreated())
	}
	// A failed check keeps when the digest was first seen.
	now = t0.Add(90 * time.Minute)
	rt.remote = map[string]string{}
	if _, err := checker.Check(ctx, env, ""); err != nil {
		t.Fatal(err)
	}
	rt.remote = map[string]string{"docker.io/library/nginx:1": "sha256:new"}
	if _, err := checker.Check(ctx, env, ""); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(2 * time.Hour)
	if err := u.Run(ctx, env); err != nil || recreated() != 3 {
		t.Fatalf("after the delay: %v, %d recreated", err, recreated())
	}

	// More updates than maxBatch wait for approval.
	if rec := send(t, r, http.MethodPut, "/api/v1/environments/local/settings/auto-updates", `{"delaySeconds":0,"maxBatch":1}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if err := u.Run(ctx, env); err != nil || recreated() != 3 {
		t.Fatalf("over the threshold: %v, %d recreated", err, recreated())
	}
	var log AutoUpdatesLog
	if rec := do(t, r, "/api/v1/environments/local/auto-updates?namespace=default", &log); rec.Code != http.StatusOK || log.Pending == nil || len(log.Pending.Targets) != 3 {
		t.Fatalf("pending: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/environments/local/auto-updates/approve", ``); rec.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for recreated() != 6 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if recreated() != 6 {
		t.Fatalf("approved: %d recreated", recreated())
	}
	if _, ok := u.PendingFor("local"); ok {
		t.Error("still pending")
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/environments/local/auto-updates/approve", ``); rec.Code != http.StatusConflict {
		t.Errorf("approve nothing: %d", rec.Code)
	}
}
