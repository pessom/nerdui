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
	"github.com/pessom/nerdui/internal/webhooks"
)

func TestIncomingWebhooks(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	stackSvc := &stacks.Service{Dir: t.TempDir(), Registry: st.Read(), Writer: st.Write()}
	if _, err := stackSvc.Create(ctx, "shop", []byte("services: {web: {image: nginx}}\n")); err != nil {
		t.Fatal(err)
	}
	rt := writable()
	rt.containers = []runtime.Container{{ID: "c1", Name: "api"}}
	rt.spec = runtime.ContainerSpec{ID: "c1", Spec: runtime.CreateSpec{Name: "api", Image: "acme/api:latest"}}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	envs := environment.NewRegistry(environment.Local(rt, "default"))
	svc := &webhooks.Service{Store: st.Write(), Environments: envs, Stacks: stackSvc, Jobs: m}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/hooks/{token}", IncomingWebhook(svc, nil))
		h := &Handler{Envs: envs, Jobs: m, Stacks: stackSvc, Webhooks: svc}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	create := func(body string) (webhooks.Webhook, string) {
		t.Helper()
		rec := send(t, r, http.MethodPost, "/api/v1/webhooks", body)
		var resp struct {
			Webhook webhooks.Webhook
			Path    string
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if rec.Code != http.StatusCreated || !strings.HasPrefix(resp.Path, "/api/v1/hooks/") {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		return resp.Webhook, resp.Path
	}
	call := func(path string, want int) jobs.Job {
		t.Helper()
		rec := send(t, r, http.MethodPost, path, `{"anything":true}`)
		if rec.Code != want {
			t.Fatalf("call %s: %d %s", path, rec.Code, rec.Body)
		}
		if want != http.StatusAccepted {
			return jobs.Job{}
		}
		var resp struct{ Job string }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return waitDone(t, m, resp.Job)
	}

	for _, body := range []string{
		`{"name":"","environment":"local","namespace":"default","targetKind":"stack","target":"shop","action":"redeploy"}`,
		`{"name":"x","environment":"mars","namespace":"default","targetKind":"stack","target":"shop","action":"redeploy"}`,
		`{"name":"x","environment":"local","namespace":"bad ns","targetKind":"stack","target":"shop","action":"redeploy"}`,
		`{"name":"x","environment":"local","namespace":"default","targetKind":"volume","target":"shop","action":"redeploy"}`,
		`{"name":"x","environment":"local","namespace":"default","targetKind":"stack","target":"../x","action":"redeploy"}`,
		`{"name":"x","environment":"local","namespace":"default","targetKind":"stack","target":"shop","action":"delete"}`,
	} {
		if rec := send(t, r, http.MethodPost, "/api/v1/webhooks", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}

	// A stack redeploy pulls and brings the stack up.
	stack, stackHook := create(`{"name":"shop","environment":"local","namespace":"default","targetKind":"stack","target":"shop","action":"redeploy","enabled":true}`)
	if j := call(stackHook, http.StatusAccepted); j.Status != jobs.StatusSucceeded || j.User != "webhook shop" ||
		len(rt.composeRuns) != 2 || !strings.HasPrefix(rt.composeRuns[0], "pull shop") || !strings.HasPrefix(rt.composeRuns[1], "up shop") {
		t.Fatalf("stack job %+v %v", j, rt.composeRuns)
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/webhooks", `{"name":"shop","environment":"local","namespace":"default","targetKind":"stack","target":"shop","action":"restart"}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d %s", rec.Code, rec.Body)
	}

	// A container redeploy pulls and recreates with the same settings; restart restarts.
	_, apiHook := create(`{"name":"api","environment":"local","namespace":"default","targetKind":"container","target":"api","action":"redeploy","enabled":true}`)
	if j := call(apiHook, http.StatusAccepted); j.Status != jobs.StatusSucceeded || !rt.recreatePull || len(rt.recreated) != 1 || rt.recreated[0].Image != "acme/api:latest" {
		t.Fatalf("container job %+v %v", j, rt.recreated)
	}
	restart, restartHook := create(`{"name":"api-restart","environment":"local","namespace":"default","targetKind":"container","target":"api","action":"restart","enabled":true}`)
	if j := call(restartHook, http.StatusAccepted); j.Status != jobs.StatusSucceeded || len(rt.actions) != 1 || rt.actions[0].action != runtime.ActionRestart {
		t.Fatalf("restart job %+v %+v", j, rt.actions)
	}

	// The list never shows tokens and records calls.
	rec := send(t, r, http.MethodGet, "/api/v1/webhooks", "")
	for _, p := range []string{stackHook, apiHook, restartHook} {
		if strings.Contains(rec.Body.String(), strings.TrimPrefix(p, "/api/v1/hooks/")) {
			t.Fatalf("token in list: %s", rec.Body)
		}
	}
	var list struct{ Webhooks []webhooks.Webhook }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if i := slices.IndexFunc(list.Webhooks, func(w webhooks.Webhook) bool { return w.ID == stack.ID }); i < 0 || list.Webhooks[i].LastCalledAt == nil || list.Webhooks[i].LastJob == "" {
		t.Errorf("list %+v", list.Webhooks)
	}

	// A missing container fails the call and is recorded.
	rt.mu.Lock()
	rt.containers = nil
	rt.mu.Unlock()
	call(restartHook, http.StatusConflict)

	// Disabled, replaced and deleted webhooks are not found.
	if rec := send(t, r, http.MethodPut, "/api/v1/webhooks/"+jsonInt(restart.ID), `{"name":"api-restart","environment":"local","namespace":"default","targetKind":"container","target":"api","action":"restart","enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body)
	}
	call(restartHook, http.StatusNotFound)
	rec = send(t, r, http.MethodPost, "/api/v1/webhooks/"+jsonInt(stack.ID)+"/token", "")
	var replaced struct{ Path string }
	_ = json.Unmarshal(rec.Body.Bytes(), &replaced)
	if rec.Code != http.StatusOK || replaced.Path == stackHook {
		t.Fatalf("new token: %d %s", rec.Code, rec.Body)
	}
	call(stackHook, http.StatusNotFound)
	call("/api/v1/hooks/nope", http.StatusNotFound)

	// Read-only environments run nothing.
	rt.mu.Lock()
	rt.nerdctl = runtime.NerdctlStatus{Version: "1.0.0", Required: "2.3"}
	rt.mu.Unlock()
	call(replaced.Path, http.StatusServiceUnavailable)

	if rec := send(t, r, http.MethodDelete, "/api/v1/webhooks/"+jsonInt(stack.ID), ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	call(replaced.Path, http.StatusNotFound)
}

func waitDone(t *testing.T, m *jobs.Manager, id string) jobs.Job {
	t.Helper()
	for range 1000 {
		if j, ok := m.Get(id); ok && j.Done() {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s not done", id)
	return jobs.Job{}
}
