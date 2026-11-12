// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

type fakeRuntime struct {
	v          runtime.Versions
	ns         []string
	err        error
	nerdctl    runtime.NerdctlStatus
	events     runtime.EventSource
	containers []runtime.Container
	gotNS      string

	mu          sync.Mutex
	actions     []actionCall
	actionErr   map[string]error // by container id
	inspect     map[string]any   // by container ref
	logs        []runtime.LogLine
	logErr      error // returned before any line
	logFollow   bool  // with Follow, block until ctx ends after the lines
	gotLogs     runtime.LogOptions
	gotSize     bool
	info        runtime.Info
	images      []runtime.Image
	pulls       []pullCall
	pullRelease chan struct{}
	pullErr     error
}

type actionCall struct {
	ns, id string
	action runtime.Action
	opts   runtime.ActionOptions
}

func (f *fakeRuntime) System() runtime.System { return f }
func (f *fakeRuntime) Close() error           { return nil }
func (f *fakeRuntime) Events() runtime.EventSource {
	if f.events == nil {
		return &fakeSource{}
	}
	return f.events
}
func (f *fakeRuntime) Versions(context.Context) (runtime.Versions, error) {
	return f.v, f.err
}
func (f *fakeRuntime) Namespaces(context.Context) ([]string, error)  { return f.ns, f.err }
func (f *fakeRuntime) Nerdctl(context.Context) runtime.NerdctlStatus { return f.nerdctl }
func (f *fakeRuntime) Containers() runtime.Containers                { return f }
func (f *fakeRuntime) Info(context.Context) (runtime.Info, error)    { return f.info, f.err }
func (f *fakeRuntime) Images() runtime.Images                        { return fakeImages{f} }

type fakeImages struct{ f *fakeRuntime }

func (fi fakeImages) List(_ context.Context, ns string) ([]runtime.Image, error) {
	fi.f.gotNS = ns
	return fi.f.images, fi.f.err
}

// Pull writes pullOutput, then waits for pullRelease (when set) or cancellation.
func (fi fakeImages) Pull(ctx context.Context, ns, ref string, opts runtime.PullOptions, out io.Writer) error {
	fi.f.mu.Lock()
	fi.f.pulls = append(fi.f.pulls, pullCall{ns, ref, opts})
	release := fi.f.pullRelease
	fi.f.mu.Unlock()
	_, _ = io.WriteString(out, "pulling "+ref+"\n")
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fi.f.pullErr
}

type pullCall struct {
	ns, ref string
	opts    runtime.PullOptions
}

func (f *fakeRuntime) List(_ context.Context, ns string) ([]runtime.Container, error) {
	f.gotNS = ns
	return f.containers, f.err
}
func (f *fakeRuntime) Do(_ context.Context, ns, id string, a runtime.Action, o runtime.ActionOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, actionCall{ns, id, a, o})
	return f.actionErr[id]
}

func (f *fakeRuntime) Inspect(_ context.Context, ns, ref string, size bool) (any, error) {
	f.gotNS, f.gotSize = ns, size
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.inspect[ref]
	if !ok {
		return nil, fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, ref)
	}
	return v, nil
}

func (f *fakeRuntime) Logs(ctx context.Context, ns, _ string, o runtime.LogOptions, emit runtime.LogEmitter) error {
	f.mu.Lock()
	f.gotNS, f.gotLogs = ns, o
	f.mu.Unlock()
	if f.logErr != nil {
		return f.logErr
	}
	for _, l := range f.logs {
		if err := emit(l); err != nil {
			return err
		}
	}
	if o.Follow && f.logFollow {
		<-ctx.Done()
	}
	return nil
}

func newRouter(rt runtime.Runtime) http.Handler {
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: jobs.NewManager()}
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

func TestInfo(t *testing.T) {
	want := runtime.Info{
		ID: "uuid", KernelVersion: "6.8.0", CPUs: 4, MemoryBytes: 8 << 30,
		Components: []runtime.Component{{Name: "runc", Version: "1.3.0"}},
		Plugins:    []runtime.Plugin{{Type: "io.containerd.snapshotter.v1", ID: "overlayfs", Status: runtime.PluginOK}},
	}
	var got runtime.Info
	rec := do(t, newRouter(&fakeRuntime{info: want}), "/api/v1/environments/local/info", &got)
	if rec.Code != http.StatusOK || got.ID != "uuid" || got.CPUs != 4 || got.MemoryBytes != 8<<30 ||
		len(got.Components) != 1 || got.Plugins[0].ID != "overlayfs" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, field := range []string{`"kernelVersion":"6.8.0"`, `"memoryBytes":`, `"status":"ok"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("body lacks %s: %s", field, rec.Body)
		}
	}

	rec = do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/info", nil)
	if rec.Code != http.StatusServiceUnavailable {
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
