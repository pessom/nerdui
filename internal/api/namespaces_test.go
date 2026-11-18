// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestNamespaceCreateRemove(t *testing.T) {
	rt := &fakeRuntime{ns: []string{"default"}, nerdctl: runtime.NerdctlStatus{Compatible: true}}
	h := newRouter(rt)

	rec := send(t, h, http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":"lab"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"lab"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, h, http.MethodDelete, "/api/v1/environments/local/namespaces/lab", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if want := []string{"create lab", "remove lab"}; !slices.Equal(rt.nsCalls, want) {
		t.Errorf("calls %v, want %v", rt.nsCalls, want)
	}

	rec = send(t, h, http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":"lab","x":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}

	// Read-only environments list namespaces but do not change them.
	ro := &fakeRuntime{ns: []string{"default"}}
	rec = send(t, newRouter(ro), http.MethodPost, "/api/v1/environments/local/namespaces", `{"name":"lab"}`)
	if rec.Code != http.StatusServiceUnavailable || len(ro.nsCalls) != 0 {
		t.Errorf("read-only: %d %v", rec.Code, ro.nsCalls)
	}
}

func TestNamespaceErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: namespace \"lab\" still has 2 containers; remove them first", errdefs.ErrFailedPrecondition), http.StatusConflict},
		{fmt.Errorf("%w: namespace name", errdefs.ErrInvalidArgument), http.StatusBadRequest},
		{fmt.Errorf("%w: namespace \"x\" not found", errdefs.ErrNotFound), http.StatusNotFound},
		{fmt.Errorf("%w: exists", errdefs.ErrAlreadyExists), http.StatusConflict},
	} {
		h := newRouter(&fakeRuntime{nsErr: tc.err, nerdctl: runtime.NerdctlStatus{Compatible: true}})
		rec := send(t, h, http.MethodDelete, "/api/v1/environments/local/namespaces/lab", "")
		if rec.Code != tc.want {
			t.Errorf("%v: %d %s", tc.err, rec.Code, rec.Body)
		}
	}
}
