// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

type fakeRuntime struct {
	v       runtime.Versions
	ns      []string
	err     error
	nerdctl runtime.NerdctlStatus
}

func (f *fakeRuntime) System() runtime.System { return f }
func (f *fakeRuntime) Close() error           { return nil }
func (f *fakeRuntime) Versions(context.Context) (runtime.Versions, error) {
	return f.v, f.err
}
func (f *fakeRuntime) Namespaces(context.Context) ([]string, error)  { return f.ns, f.err }
func (f *fakeRuntime) Nerdctl(context.Context) runtime.NerdctlStatus { return f.nerdctl }

func newRouter(rt runtime.Runtime) http.Handler {
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default"))}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	return r
}

func do(t *testing.T, h http.Handler, path string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
	}
	return rec
}

func TestListEnvironments(t *testing.T) {
	var got struct{ Environments []Environment }
	rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments", &got)
	if rec.Code != http.StatusOK || len(got.Environments) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := Environment{ID: "local", Name: "Local host", Kind: "local", DefaultNamespace: "default"}
	if got.Environments[0] != want {
		t.Errorf("got %+v, want %+v", got.Environments[0], want)
	}
}

func TestGetEnvironment(t *testing.T) {
	for _, path := range []string{"/api/v1/environments/local", "/api/v1/environments/local/"} {
		var got EnvironmentStatus
		rec := do(t, newRouter(&fakeRuntime{v: runtime.Versions{Containerd: "v2.4.2"}}), path, &got)
		if rec.Code != http.StatusOK || got.Status != StatusConnected || got.Runtime == nil || got.Runtime.Containerd != "v2.4.2" {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}

	var got EnvironmentStatus
	down := &fakeRuntime{err: runtime.ErrNotConnected, v: runtime.Versions{Nerdctl: runtime.NerdctlStatus{Version: "2.4.2", Compatible: true}}}
	do(t, newRouter(down), "/api/v1/environments/local", &got)
	if got.Status != StatusUnavailable || got.Error == "" {
		t.Errorf("unavailable: %+v", got)
	}
	if got.Runtime == nil || got.Runtime.Nerdctl.Version != "2.4.2" {
		t.Errorf("nerdctl status missing while containerd is down: %+v", got.Runtime)
	}

	rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments/remote", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Errorf("unknown env: %d %s", rec.Code, rec.Body)
	}
}

func TestNamespaces(t *testing.T) {
	var got struct {
		Namespaces []string
		Default    string
	}
	rec := do(t, newRouter(&fakeRuntime{ns: []string{"default", "k8s.io"}}), "/api/v1/environments/local/namespaces", &got)
	if rec.Code != http.StatusOK || len(got.Namespaces) != 2 || got.Default != "default" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	rec = do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/namespaces", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Errorf("containerd down: %d %s", rec.Code, rec.Body)
	}
}

func TestNamespaceParam(t *testing.T) {
	env := environment.Local(&fakeRuntime{}, "default")
	req := func(q string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x"+q, nil)
		return r.WithContext(context.WithValue(r.Context(), envKey{}, env))
	}
	if ns, err := Namespace(req("")); err != nil || ns != "default" {
		t.Errorf("default: %q %v", ns, err)
	}
	if ns, err := Namespace(req("?namespace=k8s.io")); err != nil || ns != "k8s.io" {
		t.Errorf("explicit: %q %v", ns, err)
	}
	for _, bad := range []string{"?namespace=../etc", "?namespace=a%20b", "?namespace=-x"} {
		if _, err := Namespace(req(bad)); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: err = %v, want invalid argument", bad, err)
		}
	}
	if _, err := Namespace(httptest.NewRequest(http.MethodGet, "/x", nil)); !errdefs.IsInvalidArgument(err) {
		t.Errorf("no env and no namespace: %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("container web: %w", errdefs.ErrNotFound), http.StatusNotFound, httpjson.CodeNotFound},
		{errdefs.ErrAlreadyExists, http.StatusConflict, httpjson.CodeConflict},
		{errdefs.ErrFailedPrecondition, http.StatusConflict, httpjson.CodeConflict},
		{errdefs.ErrInvalidArgument, http.StatusBadRequest, httpjson.CodeInvalidArgument},
		{errdefs.ErrPermissionDenied, http.StatusForbidden, httpjson.CodeForbidden},
		{runtime.ErrNotConnected, http.StatusServiceUnavailable, httpjson.CodeUnavailable},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, httpjson.CodeUnavailable},
		{errdefs.ErrNotImplemented, http.StatusNotImplemented, httpjson.CodeUnavailable},
		{errors.New("boom"), http.StatusInternalServerError, httpjson.CodeInternal},
	} {
		status, code := httpjson.Status(tt.err)
		if status != tt.status || code != tt.code {
			t.Errorf("%v: got %d %s, want %d %s", tt.err, status, code, tt.status, tt.code)
		}
	}

	// Internal errors do not leak details.
	rec := httptest.NewRecorder()
	httpjson.WriteErr(rec, httptest.NewRequest(http.MethodGet, "/", nil), nil, errors.New("secret detail"))
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("internal error leaked: %s", rec.Body)
	}
}

func TestReadOnlyMode(t *testing.T) {
	newEnvRouter := func(st runtime.NerdctlStatus) http.Handler {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{nerdctl: st}, "default"))}
		r := chi.NewRouter()
		r.Route("/api/v1/environments/{env}", func(r chi.Router) {
			r.Use(h.resolveEnv, h.requireWritable)
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
			r.Get("/thing", ok)
			r.Post("/thing", ok)
			r.Delete("/thing", ok)
		})
		return r
	}
	call := func(h http.Handler, method string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/environments/local/thing", nil))
		return rec
	}

	incompatible := newEnvRouter(runtime.NerdctlStatus{Version: "2.3.5", Required: "2.4"})
	if rec := call(incompatible, http.MethodGet); rec.Code != http.StatusNoContent {
		t.Errorf("GET in read-only mode: %d", rec.Code)
	}
	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		rec := call(incompatible, m)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), httpjson.CodeNerdctlIncompatible) ||
			!strings.Contains(rec.Body.String(), "2.3.5") || !strings.Contains(rec.Body.String(), "2.4.x") {
			t.Errorf("%s in read-only mode: %d %s", m, rec.Code, rec.Body)
		}
	}

	missing := newEnvRouter(runtime.NerdctlStatus{Required: "2.4", Error: "nerdctl binary \"nerdctl\" not found"})
	if rec := call(missing, http.MethodPost); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not found") {
		t.Errorf("missing nerdctl: %d %s", rec.Code, rec.Body)
	}

	for name, st := range map[string]runtime.NerdctlStatus{
		"compatible": {Version: "2.4.0", Required: "2.4", Compatible: true},
		"skipped":    {Version: "2.3.5", Required: "2.4", CheckSkipped: true},
	} {
		if rec := call(newEnvRouter(st), http.MethodPost); rec.Code != http.StatusNoContent {
			t.Errorf("%s: POST = %d", name, rec.Code)
		}
	}
}
