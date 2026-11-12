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
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/runtime"
)

func writable() *fakeRuntime {
	return &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}}
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestContainerActions(t *testing.T) {
	for _, a := range operateActions {
		t.Run(string(a), func(t *testing.T) {
			rt := writable()
			rec := send(t, newRouter(rt), http.MethodPost, "/api/v1/environments/local/containers/abc/"+string(a)+"?namespace=k8s.io", "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			want := []actionCall{{ns: "k8s.io", id: "abc", action: a}}
			if !slices.Equal(rt.actions, want) {
				t.Errorf("calls = %+v, want %+v", rt.actions, want)
			}
		})
	}
}

func TestRemoveContainer(t *testing.T) {
	rt := writable()
	rec := send(t, newRouter(rt), http.MethodDelete, "/api/v1/environments/local/containers/abc?force=true&volumes=1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := actionCall{ns: "default", id: "abc", action: runtime.ActionRemove, opts: runtime.ActionOptions{Force: true, Volumes: true}}
	if len(rt.actions) != 1 || rt.actions[0] != want {
		t.Errorf("calls = %+v", rt.actions)
	}
	if rec := send(t, newRouter(rt), http.MethodDelete, "/api/v1/environments/local/containers/abc?force=maybe", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad force: %d", rec.Code)
	}
}

func TestContainerActionErrors(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
		msg    string
	}{
		{fmt.Errorf("%w: no such container: abc", errdefs.ErrNotFound), http.StatusNotFound, "not_found", "no such container"},
		{fmt.Errorf("%w: container abc is not running", errdefs.ErrFailedPrecondition), http.StatusConflict, "conflict", "is not running"},
		{runtime.ErrNotConnected, http.StatusServiceUnavailable, "unavailable", "containerd is unavailable"},
		// Unclassified nerdctl errors keep their message: the operator needs the reason.
		{errors.New("start abc: failed to setup network: port is already allocated"), http.StatusInternalServerError, "internal", "port is already allocated"},
	}
	for _, tt := range tests {
		rt := writable()
		rt.actionErr = map[string]error{"abc": tt.err}
		rec := send(t, newRouter(rt), http.MethodPost, "/api/v1/environments/local/containers/abc/start", "")
		var body struct {
			Error struct{ Code, Message string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tt.status || body.Error.Code != tt.code || !strings.Contains(body.Error.Message, tt.msg) {
			t.Errorf("%v: %d %s", tt.err, rec.Code, rec.Body)
		}
	}
}

func TestContainerActionsReadOnly(t *testing.T) {
	rt := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Version: "2.3.0", Required: "2.4"}}
	for _, path := range []string{"/containers/abc/stop", "/containers/actions"} {
		rec := send(t, newRouter(rt), http.MethodPost, "/api/v1/environments/local"+path, `{"action":"stop","ids":["abc"]}`)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "nerdctl_incompatible") {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if len(rt.actions) != 0 {
		t.Errorf("actions ran in read-only mode: %+v", rt.actions)
	}
}

func TestBulkContainerAction(t *testing.T) {
	rt := writable()
	rt.actionErr = map[string]error{"b": fmt.Errorf("%w: container b is not running", errdefs.ErrFailedPrecondition)}
	rec := send(t, newRouter(rt), http.MethodPost, "/api/v1/environments/local/containers/actions",
		`{"action":"remove","ids":["a","b","c"],"force":true,"volumes":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got struct {
		Action  string
		Results []BulkActionResult
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "remove" || len(got.Results) != 3 {
		t.Fatalf("%s", rec.Body)
	}
	// Results keep request order.
	for i, id := range []string{"a", "b", "c"} {
		r := got.Results[i]
		if r.ID != id || r.OK != (id != "b") {
			t.Errorf("result %d: %+v", i, r)
		}
	}
	if e := got.Results[1].Error; e == nil || e.Code != "conflict" || !strings.Contains(e.Message, "not running") {
		t.Errorf("error: %+v", got.Results[1].Error)
	}
	if len(rt.actions) != 3 || rt.actions[0].opts != (runtime.ActionOptions{Force: true, Volumes: true}) {
		t.Errorf("calls: %+v", rt.actions)
	}
}

func TestBulkContainerActionValidation(t *testing.T) {
	many := `"` + strings.Repeat(`x","`, maxBulkContainers) + `x"`
	for _, body := range []string{
		`{"action":"explode","ids":["a"]}`,
		`{"action":"stop","ids":[]}`,
		`{"action":"stop","ids":[` + many + `]}`,
		`{"action":"stop","ids":["a"],"force":true}`,
		`{"action":"stop","ids":["a"],"extra":1}`,
	} {
		rt := writable()
		if rec := send(t, newRouter(rt), http.MethodPost, "/api/v1/environments/local/containers/actions", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%.60s: %d %s", body, rec.Code, rec.Body)
		}
		if len(rt.actions) != 0 {
			t.Errorf("%.60s: actions ran", body)
		}
	}
}

// operatorOnly may operate containers but not remove them.
type operatorOnly struct{}

func (operatorOnly) Authorize(_ context.Context, _ authz.Subject, a authz.Action, _ authz.Resource) error {
	if a == authz.ContainersWrite {
		return authz.ErrForbidden
	}
	return nil
}

func TestBulkRemoveRequiresContainersWrite(t *testing.T) {
	rt := writable()
	r := chi.NewRouter()
	subject := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: "operator"}, true }
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default"))}
		h.Routes(r, authz.NewGuard(operatorOnly{}, subject, nil))
	})
	url := "/api/v1/environments/local/containers/actions"
	if rec := send(t, r, http.MethodPost, url, `{"action":"remove","ids":["a"]}`); rec.Code != http.StatusForbidden {
		t.Errorf("bulk remove: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodDelete, "/api/v1/environments/local/containers/a", ""); rec.Code != http.StatusForbidden {
		t.Errorf("remove: %d", rec.Code)
	}
	if rec := send(t, r, http.MethodPost, url, `{"action":"stop","ids":["a"]}`); rec.Code != http.StatusOK {
		t.Errorf("bulk stop: %d %s", rec.Code, rec.Body)
	}
	if len(rt.actions) != 1 || rt.actions[0].action != runtime.ActionStop {
		t.Errorf("calls: %+v", rt.actions)
	}
}
