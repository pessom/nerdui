// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
)

func TestGitWebhook(t *testing.T) {
	ctx := context.Background()
	src, commit := gitSource(t)
	commit("compose.yaml", "services: {}\n", "first")

	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	gitDir := filepath.Join(t.TempDir(), "git")
	stackSvc := &stacks.Service{Dir: t.TempDir(), Registry: st.Read(), Git: st.Read(), GitDir: gitDir}
	gitSvc := &gitstacks.Service{Dir: gitDir, Store: st.Write(), Stacks: stackSvc, AllowFile: true}
	rt := writable()
	rt.ns = []string{"default", "other"}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	envs := environment.NewRegistry(environment.Local(rt, "default"))
	var events []notify.Event
	auto := &gitstacks.Auto{Service: gitSvc, Environments: envs, Jobs: m, Notify: func(e notify.Event) { events = append(events, e) }}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/hooks/git/{token}", GitWebhook(auto, nil))
		h := &Handler{Envs: envs, Jobs: m, Stacks: stackSvc, GitStacks: gitSvc}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})

	req, err := gitSvc.Check(ctx, gitstacks.CreateRequest{Name: "web", URL: "file://" + src, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitSvc.Create(ctx, req); err != nil {
		t.Fatal(err)
	}

	const base = "/api/v1/environments/local/stacks/web/git"
	if rec := send(t, r, http.MethodPut, base+"/auto", `{"autoUpdate":"always","pollInterval":0}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad mode: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPut, base+"/auto", `{"autoUpdate":"deploy","pollInterval":7}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad interval: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPut, "/api/v1/environments/local/stacks/ghost/git/auto", `{"autoUpdate":"off","pollInterval":0}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown stack: %d %s", rec.Code, rec.Body)
	}
	rec := send(t, r, http.MethodPost, base+"/webhook", "")
	var created struct{ Path string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || !strings.HasPrefix(created.Path, "/api/v1/hooks/git/") {
		t.Fatalf("webhook: %d %s", rec.Code, rec.Body)
	}
	hook := created.Path

	// Automatic updates are off: the webhook is refused.
	if rec := send(t, r, http.MethodPost, hook, `{"ref":"refs/heads/main"}`); rec.Code != http.StatusConflict {
		t.Errorf("off: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPut, base+"/auto", `{"autoUpdate":"deploy","pollInterval":300}`); rec.Code != http.StatusNoContent {
		t.Fatalf("set auto: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, "/api/v1/hooks/git/wrong", ""); rec.Code != http.StatusNotFound {
		t.Errorf("wrong token: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, hook, `{"ref":"refs/heads/dev"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("other branch: %d %s", rec.Code, rec.Body)
	}
	ping := httptest.NewRequest(http.MethodPost, hook, strings.NewReader(`{"zen":"x"}`))
	ping.Header.Set("X-GitHub-Event", "ping")
	pong := httptest.NewRecorder()
	r.ServeHTTP(pong, ping)
	if pong.Code != http.StatusOK || !strings.Contains(pong.Body.String(), "pong") {
		t.Errorf("ping: %d %s", pong.Code, pong.Body)
	}

	// A push of a new commit redeploys where the project has containers.
	rt.mu.Lock()
	rt.containers = []runtime.Container{{ID: "w1", Compose: &runtime.ComposeRef{Project: "web", Service: "web"}}}
	rt.mu.Unlock()
	commit("compose.yaml", "services: {web: {image: nginx}}\n", "second")
	if rec := send(t, r, http.MethodPost, hook, `{"ref":"refs/heads/main"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("push: %d %s", rec.Code, rec.Body)
	}
	j := waitJob(t, m, "stack.git-pull")
	rt.mu.Lock()
	runs := slices.Clone(rt.composeRuns)
	rt.mu.Unlock()
	if j.Status != jobs.StatusSucceeded || j.User != gitstacks.AutoUser || len(runs) != 2 || !strings.HasPrefix(runs[0], "up web ") {
		t.Fatalf("job %+v runs %v", j, runs)
	}
	row, _ := gitSvc.Get(ctx, "web")
	if row.CommitMessage != "second" || row.RemoteHash != "" || len(events) != 1 || events[0].Kind != notify.KindGitUpdate {
		t.Errorf("after update %+v events %+v", row, events)
	}

	// The stack page shows the settings, never the token.
	rec = send(t, r, http.MethodGet, "/api/v1/environments/local/stacks/web", "")
	if strings.Contains(rec.Body.String(), strings.TrimPrefix(hook, "/api/v1/hooks/git/")) ||
		!strings.Contains(rec.Body.String(), `"autoUpdate":"deploy"`) || !strings.Contains(rec.Body.String(), `"webhook":true`) {
		t.Errorf("stack: %s", rec.Body)
	}
	if rec := send(t, r, http.MethodDelete, base+"/webhook", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete webhook: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, hook, ""); rec.Code != http.StatusNotFound {
		t.Errorf("removed webhook: %d %s", rec.Code, rec.Body)
	}
}

func waitJob(t *testing.T, m *jobs.Manager, kind string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.List() {
			if j.Kind == kind && j.Done() {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no finished %s job", kind)
	return jobs.Job{}
}
