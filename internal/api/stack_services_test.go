// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
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

func TestServiceOperations(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	stackSvc := &stacks.Service{Dir: t.TempDir(), Registry: st.Read(), Writer: st.Write()}
	if _, err := stackSvc.Create(ctx, "shop", []byte("services: {web: {image: nginx}, db: {image: postgres}, worker: {image: busybox}}\n")); err != nil {
		t.Fatal(err)
	}
	rt := writable()
	ref := func(svc string) *runtime.ComposeRef { return &runtime.ComposeRef{Project: "shop", Service: svc} }
	rt.containers = []runtime.Container{
		{ID: "w1", Name: "shop-web-1", Image: "docker.io/library/nginx:latest", Compose: ref("web")},
		{ID: "w2", Name: "shop-web-2", Image: "docker.io/library/nginx:latest", Compose: ref("web")},
		{ID: "d1", Name: "shop-db-1", Image: "docker.io/library/postgres:latest", Compose: ref("db")},
	}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: m, Stacks: stackSvc}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local/stacks/shop/services/"
	run := func(path string) jobs.Job {
		t.Helper()
		rec := send(t, r, http.MethodPost, base+path, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var resp struct{ Job jobs.Job }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		for range 1000 {
			if j, _ := m.Get(resp.Job.ID); j.Done() {
				return j
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s not done", path)
		return jobs.Job{}
	}

	// Restart acts on the service's containers only, not on its dependencies.
	if j := run("web/restart"); j.Status != jobs.StatusSucceeded || j.Title != "Restart service web of stack shop" {
		t.Fatalf("restart %+v", j)
	}
	if len(rt.actions) != 2 || rt.actions[0].id != "w1" || rt.actions[1].id != "w2" || rt.actions[0].action != runtime.ActionRestart || len(rt.composeRuns) != 0 {
		t.Errorf("actions %+v runs %v", rt.actions, rt.composeRuns)
	}
	if j := run("db/stop"); j.Status != jobs.StatusSucceeded || len(rt.actions) != 3 || rt.actions[2].action != runtime.ActionStop {
		t.Errorf("stop %+v %+v", j, rt.actions)
	}

	// Starting a service without containers creates it with compose.
	if j := run("worker/start"); j.Status != jobs.StatusSucceeded || len(rt.composeRuns) != 1 || !strings.HasPrefix(rt.composeRuns[0], "up shop") ||
		!slices.Equal(rt.composeOpts[0].Services, []string{"worker"}) {
		t.Errorf("start %+v %v %+v", j, rt.composeRuns, rt.composeOpts)
	}

	// Redeploy pulls and brings up the one service.
	if j := run("web/redeploy"); j.Status != jobs.StatusSucceeded || len(rt.composeRuns) != 3 ||
		!strings.HasPrefix(rt.composeRuns[1], "pull shop") || !slices.Equal(rt.composeOpts[2].Services, []string{"web"}) {
		t.Errorf("redeploy %+v %v %+v", j, rt.composeRuns, rt.composeOpts)
	}

	for path, code := range map[string]int{
		"web/explode":    http.StatusNotFound,
		"-x/restart":     http.StatusBadRequest,
		"worker/restart": http.StatusConflict, // no containers
	} {
		if rec := send(t, r, http.MethodPost, base+path, ""); rec.Code != code {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/environments/local/stacks/ghost/services/web/restart", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown stack: %d %s", rec.Code, rec.Body)
	}
}
