// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pessom/nerdui/internal/runtime"
)

type fakeRuntime struct {
	v   runtime.Versions
	err error
}

func (f fakeRuntime) Versions(context.Context) (runtime.Versions, error) { return f.v, f.err }

func newTestServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, Config{})
	resp, body := get(t, srv.URL+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"ok"`) {
		t.Errorf("body = %q", body)
	}
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name      string
		rt        Runtime
		wantCtrd  string
		wantError bool
	}{
		{"connected", fakeRuntime{v: runtime.Versions{Containerd: "v2.1.0"}}, "v2.1.0", false},
		{"disconnected", fakeRuntime{err: errors.New("boom")}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, Config{Runtime: tt.rt})
			resp, body := get(t, srv.URL+"/api/v1/version")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var got VersionResponse
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Runtime.Containerd != tt.wantCtrd {
				t.Errorf("containerd = %q, want %q", got.Runtime.Containerd, tt.wantCtrd)
			}
			if (got.Error != "") != tt.wantError {
				t.Errorf("error = %q, wantError %v", got.Error, tt.wantError)
			}
			if got.Nerdui.Version == "" {
				t.Error("nerdui version is empty")
			}
		})
	}
}

func TestSPA(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":    {Data: []byte("<html>nerdui</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	srv := newTestServer(t, Config{UI: ui})

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/", http.StatusOK, "nerdui"},
		{"/assets/app.js", http.StatusOK, "console.log"},
		{"/containers/abc", http.StatusOK, "nerdui"},
		{"/api/v1/unknown", http.StatusNotFound, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, body := get(t, srv.URL+tt.path)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want substring %q", body, tt.wantBody)
			}
		})
	}
}

func TestSPANotBuilt(t *testing.T) {
	srv := newTestServer(t, Config{UI: fstest.MapFS{".gitkeep": {}}})
	resp, body := get(t, srv.URL+"/")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "make web") {
		t.Errorf("status = %d, body = %q", resp.StatusCode, body)
	}
}
