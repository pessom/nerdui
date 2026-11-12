// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

// onlyActions allows the listed actions to everyone.
type onlyActions []authz.Action

func (o onlyActions) Authorize(_ context.Context, _ authz.Subject, a authz.Action, _ authz.Resource) error {
	for _, x := range o {
		if x == a {
			return nil
		}
	}
	return authz.ErrForbidden
}

func jobsServer(t *testing.T, f *fakeRuntime, az authz.Authorizer) (*httptest.Server, *jobs.Manager) {
	t.Helper()
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	subject := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(f, "default")), Jobs: m}
		h.Routes(r, authz.NewGuard(az, subject, nil))
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, m
}

func postJSON(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestPullJob(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, pullRelease: make(chan struct{})}
	srv, m := jobsServer(t, f, authz.AdminOnly{})
	base := srv.URL + "/api/v1"

	if resp, _ := postJSON(t, base+"/environments/local/images/pull", `{}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("no reference: %d", resp.StatusCode)
	}
	resp, body := postJSON(t, base+"/environments/local/images/pull?namespace=k8s.io", `{"reference":"nginx:1.27","platform":"linux/arm64"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("pull: %d %v", resp.StatusCode, body)
	}
	job := body["job"].(map[string]any)
	id := job["id"].(string)
	if job["status"] != "running" || job["kind"] != "image.pull" || job["namespace"] != "k8s.io" {
		t.Errorf("job %v", job)
	}
	// One pull per reference at a time.
	if resp, _ := postJSON(t, base+"/environments/local/images/pull?namespace=k8s.io", `{"reference":"nginx:1.27"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("concurrent pull: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/jobs/"+id+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	read := func() map[string]any {
		t.Helper()
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	first := read()
	data := first["data"].(map[string]any)
	if data["reset"] != true || !strings.Contains(data["output"].(string), "pulling nginx:1.27") {
		t.Fatalf("first frame %v", first)
	}
	close(f.pullRelease)
	var final map[string]any
	for {
		m := read()
		if m["type"] == "end" {
			break
		}
		if j, ok := m["data"].(map[string]any)["job"].(map[string]any); ok {
			final = j
		}
	}
	if final == nil || final["status"] != "succeeded" {
		t.Errorf("final job %v", final)
	}
	if len(f.pulls) != 1 || f.pulls[0].ns != "k8s.io" || f.pulls[0].opts.Platform != "linux/arm64" {
		t.Errorf("pulls %+v", f.pulls)
	}

	var list struct{ Jobs []jobs.Job }
	r2, _ := http.Get(base + "/jobs") //nolint:noctx // test
	_ = json.NewDecoder(r2.Body).Decode(&list)
	_ = r2.Body.Close()
	if len(list.Jobs) != 1 || list.Jobs[0].ID != id {
		t.Errorf("list %+v", list)
	}
	if j, _ := m.Get(id); j.Status != jobs.StatusSucceeded {
		t.Errorf("job %+v", j)
	}
}

func TestCancelJobAndVisibility(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, pullRelease: make(chan struct{})}
	srv, m := jobsServer(t, f, authz.AdminOnly{})
	base := srv.URL + "/api/v1"
	_, body := postJSON(t, base+"/environments/local/images/pull", `{"reference":"alpine"}`)
	id := body["job"].(map[string]any)["id"].(string)
	if resp, _ := postJSON(t, base+"/jobs/missing/cancel", ``); resp.StatusCode != http.StatusNotFound {
		t.Errorf("cancel unknown: %d", resp.StatusCode)
	}
	if resp, _ := postJSON(t, base+"/jobs/"+id+"/cancel", ``); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for j, _ := m.Get(id); !j.Done(); j, _ = m.Get(id) {
		if time.Now().After(deadline) {
			t.Fatal("not canceled")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if j, _ := m.Get(id); j.Status != jobs.StatusCanceled {
		t.Errorf("job %+v", j)
	}

	// A caller without images:write does not see the pull job.
	other, _ := jobsServer(t, f, onlyActions{authz.JobsRead})
	otherJobs := other.Config.Handler
	rec := httptest.NewRecorder()
	otherJobs.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"jobs":[]`) {
		t.Errorf("list without images:write: %d %s", rec.Code, rec.Body)
	}
}

func TestPullReadOnly(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Version: "2.3.5", Required: "2.4"}}
	srv, _ := jobsServer(t, f, authz.AdminOnly{})
	resp, body := postJSON(t, srv.URL+"/api/v1/environments/local/images/pull", `{"reference":"alpine"}`)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body["error"].(map[string]any)["code"].(string), "nerdctl_incompatible") {
		t.Errorf("read-only pull: %d %v", resp.StatusCode, body)
	}
}
