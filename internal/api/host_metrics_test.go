// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/hostmetrics"
	"github.com/pessom/nerdui/internal/store"
)

func writeProc(t *testing.T, dir, stat string) {
	t.Helper()
	for name, content := range map[string]string{
		"stat":    stat,
		"meminfo": "MemTotal: 2000 kB\nMemAvailable: 500 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		"loadavg": "1.00 0.5 0.2 1/1 1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHostMetrics serves the sampled host metrics and the alert thresholds (#64).
func TestHostMetrics(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	proc := t.TempDir()
	sampler := &hostmetrics.Sampler{Proc: proc}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")), Settings: st.Write(), HostMetrics: sampler}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const path = "/api/v1/environments/local/host/metrics"

	if rec := send(t, r, http.MethodGet, path, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before a sample: %d %s", rec.Code, rec.Body)
	}
	writeProc(t, proc, "cpu 100 0 0 100 0 0 0 0\n")
	sampler.Collect(ctx)
	writeProc(t, proc, "cpu 175 0 0 125 0 0 0 0\n")
	sampler.Collect(ctx)
	rec := send(t, r, http.MethodGet, path+"?range=6h", "")
	var got struct {
		Current         hostmetrics.Sample
		Points          []hostmetrics.Point
		Range           string
		IntervalSeconds int
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body)
	}
	if got.Current.CPU != 75 || got.Current.MemoryUsed != 1500*1024 || len(got.Points) != 1 || got.Range != "6h" || got.IntervalSeconds != 15 {
		t.Errorf("metrics %+v", got)
	}
	if rec := send(t, r, http.MethodGet, path+"?range=7d", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad range: %d", rec.Code)
	}

	const alerts = "/api/v1/settings/host-alerts"
	rec = send(t, r, http.MethodGet, alerts, "")
	var a hostmetrics.Alerts
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &a) != nil || a != hostmetrics.DefaultAlerts() {
		t.Errorf("defaults: %d %s", rec.Code, rec.Body)
	}
	body := `{"cpu":{"enabled":true,"percent":80,"minutes":10},"memory":{"enabled":false,"percent":90,"minutes":5},"disk":{"enabled":true,"percent":85,"minutes":0}}`
	if rec := send(t, r, http.MethodPut, alerts, body); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if a, err := hostmetrics.LoadAlerts(ctx, st.Read()); err != nil || !a.CPU.Enabled || a.CPU.Minutes != 10 || a.Disk.Percent != 85 {
		t.Errorf("stored %+v %v", a, err)
	}
	bad := `{"cpu":{"enabled":true,"percent":0,"minutes":1},"memory":{"percent":90},"disk":{"percent":90}}`
	if rec := send(t, r, http.MethodPut, alerts, bad); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid: %d %s", rec.Code, rec.Body)
	}
}
