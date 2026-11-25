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
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/scheduler"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
	"github.com/pessom/nerdui/internal/templates"
	"github.com/pessom/nerdui/internal/variables"
	"github.com/pessom/nerdui/internal/volbackup"
)

type fakeRuntime struct {
	v   runtime.Versions
	err error
}

func (f fakeRuntime) System() runtime.System                             { return f }
func (f fakeRuntime) Close() error                                       { return nil }
func (f fakeRuntime) Events() runtime.EventSource                        { return noEvents{} }
func (f fakeRuntime) Containers() runtime.Containers                     { return nil }
func (f fakeRuntime) Images() runtime.Images                             { return nil }
func (f fakeRuntime) Registries() runtime.Registries                     { return nil }
func (f fakeRuntime) Volumes() runtime.Volumes                           { return nil }
func (f fakeRuntime) Networks() runtime.Networks                         { return nil }
func (f fakeRuntime) Compose() runtime.Compose                           { return nil }
func (f fakeRuntime) Versions(context.Context) (runtime.Versions, error) { return f.v, f.err }
func (f fakeRuntime) Namespaces(context.Context) ([]string, error)       { return []string{"default"}, f.err }
func (f fakeRuntime) CreateNamespace(context.Context, string) error      { return f.err }
func (f fakeRuntime) RemoveNamespace(context.Context, string) error      { return f.err }
func (f fakeRuntime) Nerdctl(context.Context) runtime.NerdctlStatus {
	return runtime.NerdctlStatus{Compatible: true}
}
func (f fakeRuntime) Info(context.Context) (runtime.Info, error) { return runtime.Info{}, f.err }
func (f fakeRuntime) Summary(context.Context, bool) (runtime.Summary, error) {
	return runtime.Summary{}, f.err
}
func (f fakeRuntime) Ports(context.Context) (runtime.PortsReport, error) {
	return runtime.PortsReport{}, f.err
}
func (f fakeRuntime) PrunePreview(context.Context, string, bool) (runtime.PrunePreview, error) {
	return runtime.PrunePreview{}, f.err
}
func (f fakeRuntime) Prune(context.Context, string, runtime.PruneOptions, io.Writer) error {
	return f.err
}

// withRuntime returns a config whose local environment uses rt.
func withRuntime(rt runtime.Runtime) Config {
	return Config{Environments: environment.NewRegistry(environment.Local(rt, "default"))}
}

const testPassword = "correct horse battery"

// newTestServer starts the server with a real auth service backed by a temporary store that
// already contains the user "admin"; sessionCookie logs in and returns the cookie.
var (
	sessionCookie func(t *testing.T) *http.Cookie
	lastAuth      *auth.Service
)

func newTestServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fast := auth.Argon2Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	hash, _ := auth.HashPassword(testPassword, fast)
	if _, err := st.Write().CreateUser(ctx, db.CreateUserParams{
		Username: "admin", PasswordHash: hash, Role: auth.RoleAdmin, Now: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := auth.New(ctx, st, auth.Options{Argon2: fast, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie = func(t *testing.T) *http.Cookie {
		t.Helper()
		_, is, err := svc.Login(ctx, "admin", testPassword, auth.Client{})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.CookieName, Value: is.Token}
	}

	lastAuth = svc
	if cfg.Environments == nil {
		cfg.Environments = withRuntime(fakeRuntime{}).Environments
	}
	cfg.Auth = svc
	cfg.Logger = logger
	srv := httptest.NewServer(New(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx // test helper
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
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

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t, Config{})
	resp, _ := get(t, srv.URL+"/healthz")
	for k, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "same-origin",
		"Content-Security-Policy": ContentSecurityPolicy,
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestVersionPublicWithoutRuntime(t *testing.T) {
	srv := newTestServer(t, withRuntime(fakeRuntime{v: runtime.Versions{Containerd: "v2.4.2"}}))
	resp, body := get(t, srv.URL+"/api/v1/version")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if strings.Contains(body, "v2.4.2") || strings.Contains(body, `"runtime"`) {
		t.Errorf("runtime details exposed without a session: %s", body)
	}
	if !strings.Contains(body, `"source"`) || !strings.Contains(body, "AGPL-3.0-only") {
		t.Errorf("source/license missing (AGPL section 13): %s", body)
	}
}

func TestAuthRoutesMounted(t *testing.T) {
	srv := newTestServer(t, Config{})
	if resp, _ := get(t, srv.URL+"/api/v1/auth/me"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("me without session: %d", resp.StatusCode)
	}
	if resp, body := get(t, srv.URL+"/api/v1/auth/me", sessionCookie(t)); resp.StatusCode != http.StatusOK {
		t.Errorf("me with session: %d %s", resp.StatusCode, body)
	}
	if resp, body := get(t, srv.URL+"/api/v1/setup"); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"required":false`) {
		t.Errorf("setup status: %d %s", resp.StatusCode, body)
	}
}

func TestVersion(t *testing.T) {
	tests := []struct {
		name      string
		rt        runtime.Runtime
		wantCtrd  string
		wantError bool
	}{
		{"connected", fakeRuntime{v: runtime.Versions{Containerd: "v2.1.0"}}, "v2.1.0", false},
		{"disconnected", fakeRuntime{err: errors.New("boom")}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t, withRuntime(tt.rt))
			resp, body := get(t, srv.URL+"/api/v1/version", sessionCookie(t))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var got VersionResponse
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Runtime == nil {
				t.Fatal("runtime missing for an authenticated request")
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

func TestBasePath(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":    {Data: []byte(`<html><head><base href="/" /></head>nerdui</html>`)},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	srv := newTestServer(t, Config{UI: ui, BasePath: "/tools/nerdui"})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/tools/nerdui/", http.StatusOK, `<base href="/tools/nerdui/" />`},
		{"/tools/nerdui/containers/abc", http.StatusOK, `<base href="/tools/nerdui/" />`},
		{"/tools/nerdui/assets/app.js", http.StatusOK, "console.log"},
		{"/tools/nerdui/api/v1/version", http.StatusOK, `"nerdui"`},
		{"/tools/nerdui/healthz", http.StatusOK, "ok"},
		// A proxy that strips the prefix, and local health checks.
		{"/api/v1/version", http.StatusOK, `"nerdui"`},
		{"/healthz", http.StatusOK, "ok"},
		{"/", http.StatusOK, `<base href="/tools/nerdui/" />`},
	}
	for _, tt := range tests {
		resp, body := get(t, srv.URL+tt.path)
		if resp.StatusCode != tt.wantStatus || !strings.Contains(body, tt.wantBody) {
			t.Errorf("%s: %d %q", tt.path, resp.StatusCode, body)
		}
	}
	for path, want := range map[string]string{
		"/tools/nerdui?x=1":      "/tools/nerdui/?x=1",
		"/tools/nerdui/api/docs": "/tools/nerdui/api-docs",
	} {
		resp, err := noRedirect.Get(srv.URL + path) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 3 || resp.Header.Get("Location") != want {
			t.Errorf("%s: %d → %s, want %s", path, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
}

func TestSPANotBuilt(t *testing.T) {
	srv := newTestServer(t, Config{UI: fstest.MapFS{".gitkeep": {}}})
	resp, body := get(t, srv.URL+"/")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "make web") {
		t.Errorf("status = %d, body = %q", resp.StatusCode, body)
	}
}

// TestRouteAuthorization documents the access level of every API route and fails when a
// route is added without an authorization declaration (ADR-0009).
func TestRouteAuthorization(t *testing.T) {
	newTestServer(t, Config{})
	cfg := withRuntime(fakeRuntime{})
	cfg.Auth, cfg.Logger = lastAuth, slog.New(slog.DiscardHandler)
	cfg.Backup = &backup.Source{}
	cfg.Schedules = &scheduler.Service{}
	cfg.Variables = &variables.Service{}
	cfg.VolumeBackups = &volbackup.Service{}
	cfg.Templates = &templates.Service{}
	h := New(cfg)
	routes, err := authz.Inspect(h.(chi.Routes), APIPrefix)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range routes {
		access := string(r.Action)
		if r.Public {
			access = "public"
		}
		got[r.Method+" "+r.Path] = access
	}
	want := map[string]string{
		"GET /api/v1/version":                "public",
		"GET /api/v1/openapi.yaml":           "public",
		"GET /api/v1/openapi.json":           "public",
		"GET /api/docs":                      "public",
		"GET /api/v1/setup":                  "public",
		"POST /api/v1/setup":                 "public",
		"POST /api/v1/auth/login":            "public",
		"POST /api/v1/hooks/git/{token}":     "public",
		"POST /api/v1/hooks/{token}":         "public",
		"POST /api/v1/auth/login/mfa":        "public",
		"GET /api/v1/auth/mfa":               "session:self",
		"POST /api/v1/auth/mfa/setup":        "session:self",
		"POST /api/v1/auth/mfa/enable":       "session:self",
		"POST /api/v1/auth/mfa/backup-codes": "session:self",
		"POST /api/v1/auth/mfa/disable":      "session:self",
		"POST /api/v1/users/{id}/mfa/reset":  "users:write",
		"GET /api/v1/settings/security":      "settings:read",
		"PUT /api/v1/settings/security":      "settings:write",
		"GET /api/v1/auth/me":                "session:self",
		"POST /api/v1/auth/logout":           "session:self",
		"POST /api/v1/auth/password":         "session:self",

		"GET /api/v1/auth/preferences":             "session:self",
		"PUT /api/v1/auth/preferences":             "session:self",
		"GET /api/v1/auth/api-keys":                "session:self",
		"POST /api/v1/auth/api-keys":               "session:self",
		"DELETE /api/v1/auth/api-keys/{key}":       "session:self",
		"GET /api/v1/users/{id}/api-keys":          "users:read",
		"DELETE /api/v1/users/{id}/api-keys/{key}": "users:write",
		"GET /api/v1/users":                        "users:read",
		"POST /api/v1/users":                       "users:write",
		"GET /api/v1/users/{id}":                   "users:read",
		"PATCH /api/v1/users/{id}":                 "users:write",
		"POST /api/v1/users/{id}/password":         "users:write",
		"DELETE /api/v1/users/{id}":                "users:write",

		"GET /api/v1/environments":                                              "system:read",
		"GET /api/v1/compose-schema":                                            "stacks:read",
		"GET /api/v1/environments/{env}/":                                       "system:read",
		"GET /api/v1/environments/{env}/info":                                   "system:read",
		"GET /api/v1/environments/{env}/namespaces":                             "namespaces:read",
		"GET /api/v1/environments/{env}/containers/{id}/spec":                   "containers:read",
		"POST /api/v1/environments/{env}/containers/{id}/recreate":              "containers:write",
		"GET /api/v1/environments/{env}/image-updates":                          "images:read",
		"POST /api/v1/environments/{env}/image-updates/check":                   "images:read",
		"GET /api/v1/environments/{env}/auto-updates":                           "containers:read",
		"GET /api/v1/notifications/channels":                                    "settings:read",
		"GET /api/v1/audit":                                                     "audit:read",
		"GET /api/v1/audit/export":                                              "audit:read",
		"POST /api/v1/notifications/channels":                                   "settings:write",
		"PUT /api/v1/notifications/channels/{id}":                               "settings:write",
		"DELETE /api/v1/notifications/channels/{id}":                            "settings:write",
		"POST /api/v1/notifications/test":                                       "settings:write",
		"POST /api/v1/environments/{env}/auto-updates/approve":                  "settings:write",
		"GET /api/v1/environments/{env}/settings/auto-updates":                  "settings:read",
		"PUT /api/v1/environments/{env}/settings/auto-updates":                  "settings:write",
		"POST /api/v1/environments/{env}/namespaces":                            "namespaces:write",
		"DELETE /api/v1/environments/{env}/namespaces/{name}":                   "namespaces:write",
		"GET /api/v1/environments/{env}/events/stream":                          "system:read",
		"GET /api/v1/environments/{env}/events":                                 "system:read",
		"GET /api/v1/environments/{env}/containers":                             "containers:read",
		"GET /api/v1/environments/{env}/images":                                 "images:read",
		"GET /api/v1/environments/{env}/images/history":                         "images:read",
		"GET /api/v1/environments/{env}/images/save":                            "images:write",
		"POST /api/v1/environments/{env}/images/load":                           "images:write",
		"POST /api/v1/environments/{env}/images/pull":                           "images:write",
		"POST /api/v1/environments/{env}/images/push":                           "images:write",
		"POST /api/v1/environments/{env}/images/tag":                            "images:write",
		"POST /api/v1/environments/{env}/images/remove":                         "images:write",
		"GET /api/v1/environments/{env}/registries":                             "registries:read",
		"GET /api/v1/environments/{env}/ports":                                  "containers:read",
		"GET /api/v1/environments/{env}/summary":                                "system:read",
		"GET /api/v1/environments/{env}/stats/stream":                           "containers:read",
		"GET /api/v1/environments/{env}/stacks":                                 "stacks:read",
		"GET /api/v1/environments/{env}/stack-candidates":                       "stacks:read",
		"POST /api/v1/environments/{env}/stacks/adopt":                          "stacks:write",
		"DELETE /api/v1/environments/{env}/stacks/{name}/registration":          "stacks:write",
		"POST /api/v1/environments/{env}/stacks/git":                            "stacks:write",
		"POST /api/v1/environments/{env}/stacks/{name}/git/pull":                "stacks:deploy",
		"POST /api/v1/environments/{env}/stacks/{name}/services/{service}/{op}": "stacks:deploy",
		"DELETE /api/v1/environments/{env}/stacks/{name}/git":                   "stacks:write",
		"PUT /api/v1/environments/{env}/stacks/{name}/git/auto":                 "stacks:deploy",
		"POST /api/v1/environments/{env}/stacks/{name}/git/webhook":             "stacks:deploy",
		"DELETE /api/v1/environments/{env}/stacks/{name}/git/webhook":           "stacks:deploy",
		"GET /api/v1/git/credentials":                                           "stacks:read",
		"GET /api/v1/webhooks":                                                  "settings:read",
		"POST /api/v1/webhooks":                                                 "settings:write",
		"POST /api/v1/backup":                                                   "settings:write",
		"GET /api/v1/schedules":                                                 "settings:read",
		"POST /api/v1/schedules":                                                "settings:write",
		"PUT /api/v1/schedules/{id}":                                            "settings:write",
		"DELETE /api/v1/schedules/{id}":                                         "settings:write",
		"POST /api/v1/schedules/{id}/run":                                       "settings:write",
		"GET /api/v1/schedules/{id}/runs":                                       "settings:read",
		"GET /api/v1/templates":                                                 "stacks:read",
		"GET /api/v1/templates/detail":                                          "stacks:read",
		"POST /api/v1/templates":                                                "stacks:write",
		"PUT /api/v1/templates/{id}":                                            "stacks:write",
		"DELETE /api/v1/templates/{id}":                                         "stacks:write",
		"GET /api/v1/template-registries":                                       "settings:read",
		"PUT /api/v1/template-registries":                                       "settings:write",
		"GET /api/v1/backup-targets":                                            "settings:read",
		"POST /api/v1/backup-targets":                                           "settings:write",
		"PUT /api/v1/backup-targets/{id}":                                       "settings:write",
		"DELETE /api/v1/backup-targets/{id}":                                    "settings:write",
		"POST /api/v1/backup-targets/{id}/test":                                 "settings:write",
		"GET /api/v1/environments/{env}/volumes/{name}/backups":                 "volumes:write",
		"POST /api/v1/environments/{env}/volumes/{name}/backups":                "volumes:write",
		"POST /api/v1/environments/{env}/volumes/{name}/restore":                "volumes:write",
		"GET /api/v1/variables":                                                 "settings:read",
		"PUT /api/v1/variables/{var}":                                           "settings:write",
		"DELETE /api/v1/variables/{var}":                                        "settings:write",
		"GET /api/v1/environments/{env}/stacks/{name}/variables":                "stacks:read",
		"PUT /api/v1/environments/{env}/stacks/{name}/variables/{var}":          "stacks:write",
		"DELETE /api/v1/environments/{env}/stacks/{name}/variables/{var}":       "stacks:write",
		"PUT /api/v1/webhooks/{id}":                                             "settings:write",
		"DELETE /api/v1/webhooks/{id}":                                          "settings:write",
		"POST /api/v1/webhooks/{id}/token":                                      "settings:write",
		"POST /api/v1/git/credentials":                                          "settings:write",
		"PUT /api/v1/git/credentials/{id}":                                      "settings:write",
		"DELETE /api/v1/git/credentials/{id}":                                   "settings:write",
		"GET /api/v1/git/known-hosts":                                           "settings:read",
		"DELETE /api/v1/git/known-hosts":                                        "settings:write",
		"GET /api/v1/environments/{env}/stacks/{name}":                          "stacks:read",
		"GET /api/v1/environments/{env}/host/metrics":                           "system:read",
		"GET /api/v1/settings/host-alerts":                                      "settings:read",
		"PUT /api/v1/settings/host-alerts":                                      "settings:write",
		"DELETE /api/v1/environments/{env}/stacks/{name}":                       "stacks:write",
		"POST /api/v1/environments/{env}/stacks/{name}/{op}":                    "stacks:deploy",
		"GET /api/v1/environments/{env}/stacks/{name}/logs/stream":              "stacks:read",
		"POST /api/v1/environments/{env}/stacks":                                "stacks:write",
		"GET /api/v1/environments/{env}/stacks/{name}/files/{file}":             "stacks:read",
		"PUT /api/v1/environments/{env}/stacks/{name}/files/{file}":             "stacks:write",
		"POST /api/v1/environments/{env}/stacks/{name}/validate":                "stacks:write",
		"GET /api/v1/environments/{env}/stacks/{name}/tree":                     "stacks:read",
		"GET /api/v1/environments/{env}/stacks/{name}/tree/file":                "stacks:read",
		"PUT /api/v1/environments/{env}/stacks/{name}/tree/file":                "stacks:write",
		"DELETE /api/v1/environments/{env}/stacks/{name}/tree/file":             "stacks:write",
		"GET /api/v1/environments/{env}/networks":                               "networks:read",
		"POST /api/v1/environments/{env}/networks":                              "networks:write",
		"DELETE /api/v1/environments/{env}/networks/{name}":                     "networks:write",
		"GET /api/v1/environments/{env}/volumes":                                "volumes:read",
		"POST /api/v1/environments/{env}/volumes":                               "volumes:write",
		"DELETE /api/v1/environments/{env}/volumes/{name}":                      "volumes:write",
		"GET /api/v1/environments/{env}/volumes/{name}/files":                   "volumes:write",
		"GET /api/v1/environments/{env}/volumes/{name}/files/preview":           "volumes:write",
		"GET /api/v1/environments/{env}/volumes/{name}/files/download":          "volumes:write",
		"GET /api/v1/environments/{env}/system/prune":                           "system:read",
		"POST /api/v1/environments/{env}/system/prune":                          "system:prune",
		"POST /api/v1/environments/{env}/registries":                            "registries:write",
		"DELETE /api/v1/environments/{env}/registries":                          "registries:write",
		"GET /api/v1/jobs":                                                      "jobs:read",
		"GET /api/v1/jobs/{id}":                                                 "jobs:read",
		"GET /api/v1/jobs/{id}/stream":                                          "jobs:read",
		"POST /api/v1/jobs/{id}/cancel":                                         "jobs:read",
		"GET /api/v1/environments/{env}/containers/{id}/inspect":                "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/logs":                   "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/logs/stream":            "containers:read",
		"POST /api/v1/environments/{env}/containers/{id}/start":                 "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/stop":                  "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/restart":               "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/kill":                  "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/pause":                 "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/unpause":               "containers:operate",
		"DELETE /api/v1/environments/{env}/containers/{id}":                     "containers:write",
		"POST /api/v1/environments/{env}/containers/actions":                    "containers:operate",
		"GET /api/v1/environments/{env}/containers/{id}/top":                    "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/stats/stream":           "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/stats/history":          "containers:read",
		"GET /api/v1/environments/{env}/settings/prometheus":                    "settings:read",
		"PUT /api/v1/environments/{env}/settings/prometheus":                    "settings:write",
		"DELETE /api/v1/environments/{env}/settings/prometheus":                 "settings:write",
		"POST /api/v1/environments/{env}/settings/prometheus/test":              "settings:write",
		"GET /api/v1/environments/{env}/containers/{id}/exec":                   "containers:exec",
		"POST /api/v1/environments/{env}/containers":                            "containers:write",
		"POST /api/v1/environments/{env}/containers/command":                    "containers:write",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: access = %q, want %q", k, got[k], v)
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("new route %s (%s): add it to this table", k, v)
		}
	}
}

func TestVersionRuntimeRequiresSystemRead(t *testing.T) {
	cfg := withRuntime(fakeRuntime{v: runtime.Versions{Containerd: "v2.4.2"}})
	cfg.Authorizer = denyAll{}
	srv := newTestServer(t, cfg)
	_, body := get(t, srv.URL+"/api/v1/version", sessionCookie(t))
	if strings.Contains(body, `"runtime"`) {
		t.Errorf("runtime exposed without system:read: %s", body)
	}
}

type denyAll struct{}

func (denyAll) Authorize(context.Context, authz.Subject, authz.Action, authz.Resource) error {
	return authz.ErrForbidden
}

type noEvents struct{}

func (noEvents) Subscribe(context.Context) (<-chan runtime.Event, <-chan error, error) {
	return nil, nil, runtime.ErrNotConnected
}

func TestEventsStreamThroughFullStack(t *testing.T) {
	srv := newTestServer(t, Config{})
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/environments/local/events/stream"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Without a session: rejected before the upgrade.
	if _, resp, err := websocket.Dial(ctx, wsURL, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous upgrade: %v %v", resp, err)
	}

	hdr := http.Header{"Cookie": {sessionCookie(t).String()}}
	// Cross-origin upgrade with a valid session: rejected (CSRF protection).
	cross := http.Header{"Cookie": hdr["Cookie"], "Origin": {"https://evil.example"}}
	if _, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: cross}); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin upgrade: %v %v", resp, err)
	}

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	_, b, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(b), `"action":"disconnected"`) {
		t.Errorf("first message: %s %v", b, err)
	}
}

func TestAPISpecIsPublic(t *testing.T) {
	srv := newTestServer(t, Config{})
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, ct := range map[string]string{"/api/v1/openapi.yaml": "application/yaml", "/api/v1/openapi.json": "application/json"} {
		resp, err := noRedirect.Get(srv.URL + path) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ct || !strings.Contains(string(b), "openapi") {
			t.Errorf("%s: %d %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	resp, err := noRedirect.Get(srv.URL + "/api/docs") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/api-docs" {
		t.Errorf("/api/docs: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestRedactPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/v1/hooks/git/s3cr3t":   "/api/v1/hooks/git/<token>",
		"/api/v1/hooks/abc":          "/api/v1/hooks/<token>",
		"/api/v1/hooks/":             "/api/v1/hooks/",
		"/api/v1/environments/local": "/api/v1/environments/local",
	} {
		if got := redactPath(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
