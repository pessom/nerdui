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
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/store"
)

func settingsRouter(t *testing.T, rt *fakeRuntime) http.Handler {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")),
			Jobs: jobs.NewManager(), Settings: st.Write(),
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	return r
}

// fakePrometheus answers every range query with one point at the start and records the
// queries and Authorization headers.
type fakePrometheus struct {
	mu      sync.Mutex
	queries []string
	auth    []string
}

func (f *fakePrometheus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.queries = append(f.queries, r.URL.Query().Get("query"))
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/v1/query_range":
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%s,"7"]]}]}}`,
			r.URL.Query().Get("start"))
	case "/api/v1/query":
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"2"]}]}}`)
	default:
		http.NotFound(w, r)
	}
}

func TestPrometheusSettings(t *testing.T) {
	// Settings do not need a writable nerdctl: the runtime is read-only here.
	h := settingsRouter(t, &fakeRuntime{})
	const path = "/api/v1/environments/local/settings/prometheus"
	get := func() PrometheusSettings {
		t.Helper()
		rec := send(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("get: %d %s", rec.Code, rec.Body)
		}
		var s PrometheusSettings
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		return s
	}
	if s := get(); s.Configured {
		t.Fatalf("configured before saving: %+v", s)
	}

	rec := send(t, h, http.MethodPut, path, `{"url":"http://prom:9090/","auth":"basic","username":"u","secret":"pw"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "pw") {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	want := PrometheusSettings{Configured: true, URL: "http://prom:9090", Auth: "basic", Username: "u", HasSecret: true}
	if s := get(); s != want {
		t.Errorf("after put: %+v", s)
	}
	// An empty secret keeps the stored one for the same mode...
	if rec := send(t, h, http.MethodPut, path, `{"url":"http://prom2:9090","auth":"basic","username":"v"}`); rec.Code != http.StatusOK {
		t.Fatalf("put without secret: %d %s", rec.Code, rec.Body)
	}
	if s := get(); !s.HasSecret || s.URL != "http://prom2:9090" || s.Username != "v" {
		t.Errorf("kept secret: %+v", s)
	}
	// ...but not for another mode.
	if rec := send(t, h, http.MethodPut, path, `{"url":"http://prom2:9090","auth":"bearer"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bearer without token: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPut, path, `{"url":"file:///etc/passwd","auth":"none"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPut, path, `{"url":"http://prom:9090","extra":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d %s", rec.Code, rec.Body)
	}

	if rec := send(t, h, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if s := get(); s.Configured {
		t.Errorf("after delete: %+v", s)
	}
}

func TestPrometheusTestAndHistory(t *testing.T) {
	fake := &fakePrometheus{}
	prom := httptest.NewServer(fake)
	defer prom.Close()
	rt := writable()
	h := settingsRouter(t, rt)
	const path = "/api/v1/environments/local/settings/prometheus"

	status := func() Features {
		t.Helper()
		var st EnvironmentStatus
		_ = json.Unmarshal(send(t, h, http.MethodGet, "/api/v1/environments/local/", "").Body.Bytes(), &st)
		return st.Features
	}
	if status().MetricsHistory {
		t.Error("metricsHistory before configuring")
	}
	hist := "/api/v1/environments/local/containers/abc/stats/history?namespace=k8s.io&range=1h"
	if rec := send(t, h, http.MethodGet, hist, ""); rec.Code != http.StatusConflict {
		t.Errorf("history without prometheus: %d %s", rec.Code, rec.Body)
	}

	// Testing uses the submitted settings without saving them.
	body := fmt.Sprintf(`{"url":%q,"auth":"bearer","secret":"tok"}`, prom.URL)
	rec := send(t, h, http.MethodPost, path+"/test", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"containers":2`) {
		t.Fatalf("test: %d %s", rec.Code, rec.Body)
	}
	if status().MetricsHistory {
		t.Error("test saved the settings")
	}
	if rec := send(t, h, http.MethodPut, path, body); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if !status().MetricsHistory {
		t.Error("metricsHistory after configuring")
	}

	rec = send(t, h, http.MethodGet, hist, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Range string
		Step  int64
		Times []int64
		CPU   []*float64
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Range != "1h" || out.Step != 15 || len(out.Times) != 241 || out.CPU[0] == nil || *out.CPU[0] != 7 || out.CPU[1] != nil {
		t.Errorf("history %+v", out)
	}
	fake.mu.Lock()
	for i, q := range fake.queries {
		if strings.Contains(q, "container_id") && !strings.Contains(q, `{container_id="abc",namespace="k8s.io"}`) {
			t.Errorf("query %s", q)
		}
		if fake.auth[i] != "Bearer tok" {
			t.Errorf("auth %q", fake.auth[i])
		}
	}
	fake.mu.Unlock()

	for _, bad := range []string{
		"/api/v1/environments/local/containers/abc/stats/history?range=2h",
		"/api/v1/environments/local/containers/a%22b/stats/history?range=1h",
		"/api/v1/environments/local/containers/abc/stats/history?range=1h&namespace=a%22",
	} {
		if rec := send(t, h, http.MethodGet, bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}

	// An unreachable server is reported as unavailable.
	prom.Close()
	if rec := send(t, h, http.MethodGet, hist, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unreachable: %d %s", rec.Code, rec.Body)
	}
}
