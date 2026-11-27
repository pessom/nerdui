// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package authz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCatalog(t *testing.T) {
	seen := map[Action]bool{}
	for _, a := range All() {
		if seen[a] {
			t.Errorf("duplicate action %q", a)
		}
		seen[a] = true
		parts := strings.Split(string(a), ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Errorf("action %q is not <area>:<verb>", a)
		}
	}
	if !Known(ContainersExec) || Known("containers:fly") {
		t.Error("Known is wrong")
	}
	// All returns a copy.
	all := All()
	all[0] = "mutated"
	if All()[0] == "mutated" {
		t.Error("All exposes the internal slice")
	}
}

func TestAdminOnly(t *testing.T) {
	ctx := context.Background()
	admin := Subject{Username: "a", Role: RoleAdmin}
	for _, a := range All() {
		if err := (AdminOnly{}).Authorize(ctx, admin, a, Resource{}); err != nil {
			t.Errorf("admin denied %s: %v", a, err)
		}
	}
	if err := (AdminOnly{}).Authorize(ctx, Subject{Role: "viewer"}, ContainersRead, Resource{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("non-admin: %v", err)
	}
	if err := (AdminOnly{}).Authorize(ctx, admin, "containers:fly", Resource{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("unknown action: %v", err)
	}
}

type recordingAuthorizer struct {
	got Resource
	err error
}

func (r *recordingAuthorizer) Authorize(_ context.Context, _ Subject, _ Action, res Resource) error {
	r.got = res
	return r.err
}

func TestProtect(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	subject := Subject{Username: "alice", Role: RoleAdmin}
	withSubject := func(ok bool) SubjectFunc {
		return func(*http.Request) (Subject, bool) { return subject, ok }
	}
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

	serve := func(g *Guard) *httptest.ResponseRecorder {
		r := chi.NewRouter()
		r.Method(http.MethodPost, "/api/v1/environments/{env}/containers/{id}/stop", g.Protect(ContainersOperate, ok))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/environments/local/containers/web/stop?namespace=k8s.io", nil))
		return rec
	}

	auth := &recordingAuthorizer{}
	if rec := serve(NewGuard(auth, withSubject(true), logger)); rec.Code != http.StatusNoContent {
		t.Fatalf("allowed: %d", rec.Code)
	}
	if want := (Resource{Environment: "local", Namespace: "k8s.io", ID: "web"}); auth.got != want {
		t.Errorf("resource = %+v, want %+v", auth.got, want)
	}

	if rec := serve(NewGuard(auth, withSubject(false), logger)); rec.Code != http.StatusUnauthorized {
		t.Errorf("no subject: %d", rec.Code)
	}

	auth.err = ErrForbidden
	rec := serve(NewGuard(auth, withSubject(true), logger))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"forbidden"`) {
		t.Errorf("denied: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "authorization denied") || !strings.Contains(logs.String(), "containers:operate") {
		t.Errorf("denial not logged: %s", logs.String())
	}

	auth.err = errors.New("db down")
	if rec := serve(NewGuard(auth, withSubject(true), logger)); rec.Code != http.StatusInternalServerError {
		t.Errorf("authorizer error: %d", rec.Code)
	}
}

func TestProtectUnknownActionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Protect accepted an unknown action")
		}
	}()
	NewGuard(AdminOnly{}, nil, nil).Protect("containers:fly", func(http.ResponseWriter, *http.Request) {})
}

func TestAllowed(t *testing.T) {
	g := NewGuard(AdminOnly{}, func(*http.Request) (Subject, bool) { return Subject{Role: RoleAdmin}, true }, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !g.Allowed(req, SystemRead) {
		t.Error("admin not allowed")
	}
	anon := NewGuard(AdminOnly{}, func(*http.Request) (Subject, bool) { return Subject{}, false }, nil)
	if anon.Allowed(req, SystemRead) {
		t.Error("anonymous allowed")
	}
}

func TestInspect(t *testing.T) {
	g := NewGuard(AdminOnly{}, nil, nil)
	h := func(http.ResponseWriter, *http.Request) {}
	mw := func(next http.Handler) http.Handler { return next }

	r := chi.NewRouter()
	r.Get("/healthz", h) // outside the prefix: ignored
	r.Route("/api/v1", func(r chi.Router) {
		r.Method(http.MethodGet, "/version", Public(h))
		r.With(mw).Method(http.MethodGet, "/containers", g.Protect(ContainersRead, h))
		r.Group(func(r chi.Router) {
			r.Use(mw)
			r.Method(http.MethodDelete, "/containers/{id}", g.Protect(ContainersWrite, h))
		})
	})

	routes, err := Inspect(r, "/api/")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("routes = %+v", routes)
	}
	if routes[0].Path != "/api/v1/containers" || routes[0].Action != ContainersRead {
		t.Errorf("chained handler not unwrapped: %+v", routes[0])
	}

	r.Route("/api/v2", func(r chi.Router) {
		r.Get("/forgotten", h)
		r.With(mw).Post("/forgotten-too", h)
	})
	_, err = Inspect(r, "/api/")
	if err == nil || !strings.Contains(err.Error(), "GET /api/v2/forgotten") || !strings.Contains(err.Error(), "POST /api/v2/forgotten-too") {
		t.Errorf("undeclared routes not reported: %v", err)
	}
}

func TestActionRead(t *testing.T) {
	for _, a := range All() {
		want := strings.HasSuffix(string(a), ":read") || a == SessionSelf
		if a.Read() != want {
			t.Errorf("%s.Read() = %v", a, a.Read())
		}
	}
	for _, a := range []Action{ContainersExec, SystemPrune, StacksDeploy, UsersWrite} {
		if a.Read() {
			t.Errorf("%s must not be a read action", a)
		}
	}
}

func TestKeyAllows(t *testing.T) {
	for _, tc := range []struct {
		s    Subject
		a    Action
		want bool
	}{
		{Subject{}, ContainersWrite, true},                              // password session
		{Subject{APIKey: true}, ContainersWrite, true},                  // unrestricted key
		{Subject{APIKey: true, ReadOnly: true}, ContainersRead, true},   // read-only key reads
		{Subject{APIKey: true, ReadOnly: true}, ContainersWrite, false}, // and only reads
		{Subject{APIKey: true, Permissions: []Action{StacksDeploy}}, StacksDeploy, true},
		{Subject{APIKey: true, Permissions: []Action{StacksDeploy}}, StacksRead, false},
		{Subject{APIKey: true, Permissions: []Action{}}, SessionSelf, true}, // its own session
		{Subject{APIKey: true, ReadOnly: true, Permissions: []Action{StacksDeploy}}, StacksDeploy, false},
	} {
		if err := tc.s.KeyAllows(tc.a); (err == nil) != tc.want || (err != nil && !errors.Is(err, ErrForbidden)) {
			t.Errorf("%+v %s: %v", tc.s, tc.a, err)
		}
	}
}

func TestBuiltinRoles(t *testing.T) {
	has := func(role string, a Action) bool {
		perms, ok := Builtin(role)
		if !ok {
			t.Fatalf("role %q missing", role)
		}
		return slices.Contains(perms, a)
	}
	admin, _ := Builtin(RoleAdmin)
	if !slices.Equal(admin, All()) {
		t.Errorf("admin = %v", admin)
	}
	for _, c := range []struct {
		role string
		a    Action
		want bool
	}{
		{RoleEditor, ContainersWrite, true},
		{RoleEditor, SettingsRead, true},
		{RoleEditor, SettingsWrite, false},
		{RoleEditor, UsersRead, false},
		{RoleEditor, AuditRead, false},
		{RoleOperator, ContainersOperate, true},
		{RoleOperator, ContainersExec, true},
		{RoleOperator, StacksDeploy, true},
		{RoleOperator, ContainersWrite, false},
		{RoleOperator, StacksWrite, false},
		{RoleOperator, SettingsRead, false},
		{RoleViewer, ContainersRead, true},
		{RoleViewer, JobsRead, true},
		{RoleViewer, SessionSelf, true},
		{RoleViewer, ContainersOperate, false},
		{RoleViewer, ContainersExec, false},
		{RoleViewer, UsersRead, false},
		{RoleViewer, SettingsRead, false},
	} {
		if got := has(c.role, c.a); got != c.want {
			t.Errorf("%s %s = %v", c.role, c.a, got)
		}
	}
	if _, ok := Builtin("root"); ok {
		t.Error("unknown built-in role")
	}
	if got := Normalize([]Action{StacksDeploy, "x:y", ContainersRead, StacksDeploy}); !slices.Equal(got,
		[]Action{SessionSelf, JobsRead, ContainersRead, StacksDeploy}) {
		t.Errorf("Normalize = %v", got)
	}
}

type lookup map[string][]Action

func (l lookup) RolePermissions(_ context.Context, role string) ([]Action, error) {
	return l[role], nil
}

func TestRolesAuthorizer(t *testing.T) {
	ctx := context.Background()
	az := Roles{Lookup: lookup{"deployer": {StacksRead, StacksDeploy}}}
	if err := az.Authorize(ctx, Subject{Role: "deployer"}, StacksDeploy, Resource{}); err != nil {
		t.Error(err)
	}
	for _, c := range []struct {
		role string
		a    Action
	}{{"deployer", StacksWrite}, {"gone", ContainersRead}, {"deployer", "stacks:fly"}} {
		if err := az.Authorize(ctx, Subject{Role: c.role}, c.a, Resource{}); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s %s: %v", c.role, c.a, err)
		}
	}
}
