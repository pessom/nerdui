// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/audit"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
)

func TestAuditLog(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rec := &audit.Recorder{Store: st.Write()}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	m.OnFinish = rec.JobFinished

	f := &fakeRuntime{
		ns: []string{"default"}, nerdctl: runtime.NerdctlStatus{Compatible: true}, createID: strings.Repeat("c", 64),
		spec: runtime.ContainerSpec{ID: strings.Repeat("b", 64), Spec: runtime.CreateSpec{Image: "nginx:1", Name: "web"}},
	}
	// "bob" may only read containers and the audit log; everyone else is an administrator.
	az := authorizerFunc(func(s authz.Subject, a authz.Action) error {
		if s.Username == "bob" && a != authz.ContainersRead && a != authz.AuditRead {
			return authz.ErrForbidden
		}
		return nil
	})
	subject := func(r *http.Request) (authz.Subject, bool) {
		u := r.Header.Get("X-Test-User")
		return authz.Subject{Username: u, Role: authz.RoleAdmin}, u != ""
	}
	guard := authz.NewGuard(az, subject, nil)
	guard.Auditor = rec
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		(&Handler{Envs: environment.NewRegistry(environment.Local(f, "default")), Jobs: m, Audit: rec}).Routes(r, guard)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	call := func(user, method, path, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body)) //nolint:noctx // test
		req.Header.Set("X-Test-User", user)
		req.Header.Set("User-Agent", "audit-test")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp
	}

	call("alice", http.MethodGet, "/api/v1/environments/local/containers?namespace=default", "")           // a read: not recorded
	call("alice", http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":"lab"}`)              // ok
	call("alice", http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":`)                    // failed
	call("bob", http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":"x"}`)                  // denied
	call("alice", http.MethodPost, "/api/v1/environments/local/containers/web/recreate?namespace=default", // job
		`{"image":"nginx:1","name":"web","env":[{"name":"PASSWORD","value":"hunter2"}],"pull":true}`)

	var entries []audit.Entry
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err = rec.List(ctx, audit.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if (len(entries) > 0 && entries[0].Result != audit.ResultAccepted) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(entries) != 4 {
		t.Fatalf("entries %+v", entries)
	}
	job, denied, failed, ok := entries[0], entries[1], entries[2], entries[3]
	if ok.User != "alice" || ok.Action != "namespaces:write" || ok.Result != audit.ResultOK || ok.Method != http.MethodPost ||
		ok.Path != "/api/v1/environments/{env}/namespaces" || ok.Environment != "local" || ok.ClientIP == "" || ok.UserAgent != "audit-test" {
		t.Errorf("ok %+v", ok)
	}
	if failed.Result != audit.ResultFailed || failed.Status != http.StatusBadRequest || failed.Message == "" {
		t.Errorf("failed %+v", failed)
	}
	if denied.User != "bob" || denied.Result != audit.ResultDenied || denied.Status != http.StatusForbidden {
		t.Errorf("denied %+v", denied)
	}
	if job.Result != audit.ResultOK || job.JobID == "" || job.Object != "web" || job.Namespace != "default" ||
		strings.Contains(job.Message+job.Path+job.Object, "hunter2") {
		t.Errorf("job %+v", job)
	}

	// Filters and the API.
	if got, _ := rec.List(ctx, audit.Filter{User: "bob"}); len(got) != 1 {
		t.Errorf("by user %+v", got)
	}
	if got, _ := rec.List(ctx, audit.Filter{Query: "RECREATE"}); len(got) != 1 {
		t.Errorf("by query %+v", got)
	}
	if got, _ := rec.List(ctx, audit.Filter{Before: entries[1].ID, Limit: 1}); len(got) != 1 || got[0].ID != entries[2].ID {
		t.Errorf("paging %+v", got)
	}
	if resp := call("bob", http.MethodGet, "/api/v1/audit?result=denied", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("list: %d", resp.StatusCode)
	}
	if resp := call("bob", http.MethodGet, "/api/v1/audit?since=yesterday", ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad since: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/audit/export?user=alice", nil) //nolint:noctx // test
	req.Header.Set("X-Test-User", "alice")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rows, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil || len(rows) != 4 || rows[0][0] != "time" || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("export %v %v", rows, err)
	}
}

type authorizerFunc func(authz.Subject, authz.Action) error

func (f authorizerFunc) Authorize(_ context.Context, s authz.Subject, a authz.Action, _ authz.Resource) error {
	return f(s, a)
}
