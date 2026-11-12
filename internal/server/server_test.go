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
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

type fakeRuntime struct {
	v   runtime.Versions
	err error
}

func (f fakeRuntime) System() runtime.System                             { return f }
func (f fakeRuntime) Close() error                                       { return nil }
func (f fakeRuntime) Events() runtime.EventSource                        { return noEvents{} }
func (f fakeRuntime) Containers() runtime.Containers                     { return nil }
func (f fakeRuntime) Versions(context.Context) (runtime.Versions, error) { return f.v, f.err }
func (f fakeRuntime) Namespaces(context.Context) ([]string, error)       { return []string{"default"}, f.err }
func (f fakeRuntime) Nerdctl(context.Context) runtime.NerdctlStatus {
	return runtime.NerdctlStatus{Compatible: true}
}
func (f fakeRuntime) Info(context.Context) (runtime.Info, error) { return runtime.Info{}, f.err }

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
		"GET /api/v1/version":        "public",
		"GET /api/v1/setup":          "public",
		"POST /api/v1/setup":         "public",
		"POST /api/v1/auth/login":    "public",
		"GET /api/v1/auth/me":        "session:self",
		"POST /api/v1/auth/logout":   "session:self",
		"POST /api/v1/auth/password": "session:self",

		"GET /api/v1/users":                "users:read",
		"POST /api/v1/users":               "users:write",
		"GET /api/v1/users/{id}":           "users:read",
		"PATCH /api/v1/users/{id}":         "users:write",
		"POST /api/v1/users/{id}/password": "users:write",
		"DELETE /api/v1/users/{id}":        "users:write",

		"GET /api/v1/environments":                                   "system:read",
		"GET /api/v1/environments/{env}/":                            "system:read",
		"GET /api/v1/environments/{env}/info":                        "system:read",
		"GET /api/v1/environments/{env}/namespaces":                  "system:read",
		"GET /api/v1/environments/{env}/events/stream":               "system:read",
		"GET /api/v1/environments/{env}/containers":                  "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/inspect":     "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/logs":        "containers:read",
		"GET /api/v1/environments/{env}/containers/{id}/logs/stream": "containers:read",
		"POST /api/v1/environments/{env}/containers/{id}/start":      "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/stop":       "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/restart":    "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/kill":       "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/pause":      "containers:operate",
		"POST /api/v1/environments/{env}/containers/{id}/unpause":    "containers:operate",
		"DELETE /api/v1/environments/{env}/containers/{id}":          "containers:write",
		"POST /api/v1/environments/{env}/containers/actions":         "containers:operate",
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
