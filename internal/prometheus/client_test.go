// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

func TestConfigValidate(t *testing.T) {
	ok := []struct {
		in   Config
		want Config
	}{
		{Config{URL: "http://127.0.0.1:9090/"}, Config{URL: "http://127.0.0.1:9090", Auth: AuthNone}},
		{Config{URL: " https://m.example/prometheus/ ", Auth: AuthNone, Username: "x", Secret: "y"},
			Config{URL: "https://m.example/prometheus", Auth: AuthNone}},
		{Config{URL: "http://p:9090", Auth: AuthBasic, Username: "u", Secret: "s"},
			Config{URL: "http://p:9090", Auth: AuthBasic, Username: "u", Secret: "s"}},
		{Config{URL: "http://p:9090", Auth: AuthBearer, Username: "u", Secret: "t"},
			Config{URL: "http://p:9090", Auth: AuthBearer, Secret: "t"}},
	}
	for _, tc := range ok {
		c := tc.in
		if err := c.Validate(); err != nil || c != tc.want {
			t.Errorf("Validate(%+v) = %+v, %v; want %+v", tc.in, c, err, tc.want)
		}
	}
	bad := []Config{
		{URL: ""},
		{URL: "ftp://p"},
		{URL: "http://"},
		{URL: "http://u:p@p:9090"},
		{URL: "http://p:9090?x=1"},
		{URL: "http://p:9090", Auth: "digest"},
		{URL: "http://p:9090", Auth: AuthBasic, Secret: "s"},
		{URL: "http://p:9090", Auth: AuthBearer},
	}
	for _, c := range bad {
		if err := c.Validate(); !errdefs.IsInvalidArgument(err) {
			t.Errorf("Validate(%+v) = %v, want invalid argument", c, err)
		}
	}
}

func TestQueriesQuoteLabels(t *testing.T) {
	q, err := Queries("k8s.io", "abc123", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := `max(rate(container_cpu_usage_usec_microseconds{container_id="abc123",namespace="k8s.io"}[120s])) / 10000`
	if q["cpu"] != want {
		t.Errorf("cpu query %s", q["cpu"])
	}
	if len(q) != 6 {
		t.Errorf("queries %v", q)
	}
	for _, id := range []string{`a"}`, "a b", "", strings.Repeat("a", 100)} {
		if _, err := Queries("default", id, time.Minute); !errdefs.IsInvalidArgument(err) {
			t.Errorf("id %q: %v", id, err)
		}
	}
	if _, err := Queries(`x"`, "abc", time.Minute); !errdefs.IsInvalidArgument(err) {
		t.Errorf("namespace: %v", err)
	}
}

// fakeProm answers range and instant queries; it records the queries and auth headers.
type fakeProm struct {
	mu      sync.Mutex
	queries []string
	auth    []string
	handle  func(w http.ResponseWriter, r *http.Request) bool
}

func (f *fakeProm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.queries = append(f.queries, r.URL.Query().Get("query"))
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	if f.handle != nil && f.handle(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/prom/api/v1/query_range":
		var start int64
		_, _ = fmt.Sscan(r.URL.Query().Get("start"), &start)
		q := r.URL.Query().Get("query")
		switch {
		case strings.Contains(q, MetricCPU):
			// Two points: the first and the third step; a NaN is dropped.
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%d,"12.5"],[%d,"25"],[%d,"NaN"]]}]}}`, start, start+30, start+45)
		case strings.Contains(q, MetricMemoryLimit):
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		default:
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%d,"1"]]}]}}`, start)
		}
	case "/prom/api/v1/query":
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"3"]}]}}`)
	case "/prom/api/v1/status/buildinfo":
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"version":"3.5.0"}}`)
	default:
		http.NotFound(w, r)
	}
}

func TestHistory(t *testing.T) {
	fake := &fakeProm{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(Config{URL: srv.URL + "/prom", Auth: AuthBearer, Secret: "tok"}, nil)
	r, _ := RangeByName("1h")
	// 10:00:07 aligns to 10:00:00 (step 15 s).
	end := time.Date(2027, 3, 1, 10, 0, 7, 0, time.UTC)
	h, err := c.History(context.Background(), "default", "abc", r, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Times) != 241 || h.Step != 15 || h.Range != "1h" {
		t.Fatalf("axis: %d points, step %d, range %s", len(h.Times), h.Step, h.Range)
	}
	if last := h.Times[len(h.Times)-1]; last != time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("last point %d", last)
	}
	if h.CPU[0] == nil || *h.CPU[0] != 12.5 || h.CPU[1] != nil || h.CPU[2] == nil || *h.CPU[2] != 25 || h.CPU[3] != nil {
		t.Errorf("cpu %v %v %v %v", h.CPU[0], h.CPU[1], h.CPU[2], h.CPU[3])
	}
	for name, col := range map[string][]*float64{"memoryLimit": h.MemoryLimit, "pids": h.PIDs} {
		if len(col) != len(h.Times) {
			t.Errorf("%s has %d points", name, len(col))
		}
	}
	if h.MemoryLimit[0] != nil || h.PIDs[0] == nil || *h.PIDs[0] != 1 {
		t.Errorf("memoryLimit %v pids %v", h.MemoryLimit[0], h.PIDs[0])
	}
	if len(fake.queries) != 6 {
		t.Errorf("queries %v", fake.queries)
	}
	for _, a := range fake.auth {
		if a != "Bearer tok" {
			t.Errorf("authorization %q", a)
		}
	}

	version, n, err := c.Check(context.Background())
	if err != nil || version != "3.5.0" || n != 3 {
		t.Errorf("check: %q %d %v", version, n, err)
	}
}

func TestClientErrors(t *testing.T) {
	cases := map[string]struct {
		handle func(w http.ResponseWriter) bool
		want   string
	}{
		"unauthorized":   {func(w http.ResponseWriter) bool { w.WriteHeader(http.StatusUnauthorized); return true }, "rejected the credentials"},
		"not prometheus": {func(w http.ResponseWriter) bool { _, _ = w.Write([]byte("<html>")); return true }, "not a Prometheus API response"},
		"query error": {func(w http.ResponseWriter) bool {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`))
			return true
		}, "prometheus: parse error"},
		"too large": {func(w http.ResponseWriter) bool {
			_, _ = w.Write([]byte(`{"status":"success","data":"` + strings.Repeat("x", maxResponse) + `"}`))
			return true
		}, "larger than"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(&fakeProm{handle: func(w http.ResponseWriter, _ *http.Request) bool { return tc.handle(w) }})
			defer srv.Close()
			_, err := New(Config{URL: srv.URL, Auth: AuthNone}, nil).Query(context.Background(), "up")
			if !errdefs.IsUnavailable(err) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want %q", err, tc.want)
			}
		})
	}

	// Unreachable: the error does not repeat the URL with the query.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := New(Config{URL: url, Auth: AuthBasic, Username: "u", Secret: "pw"}, nil).Query(context.Background(), "secret_query")
	if !errdefs.IsUnavailable(err) || strings.Contains(err.Error(), "secret_query") || strings.Contains(err.Error(), "pw") {
		t.Errorf("unreachable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(Config{URL: url}, nil).Query(ctx, "up"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}
