// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/updates"
)

func updatesRouter(t *testing.T, rt *fakeRuntime) http.Handler {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	envs := environment.NewRegistry(environment.Local(rt, "default"))
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: envs, Jobs: jobs.NewManager(),
			ImageUpdates: &updates.Checker{Environments: envs, Store: st.Write()},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	return r
}

func TestImageUpdates(t *testing.T) {
	rt := &fakeRuntime{
		ns: []string{"default"},
		images: []runtime.Image{
			{Name: "docker.io/library/nginx:1", Digest: "sha256:old"},
			{Name: "docker.io/library/redis:7", Digest: "sha256:same"},
			{Name: "docker.io/library/alpine:3", Digest: "sha256:a"}, // not used by containers
			{Name: "local/app:dev", Digest: "sha256:local"},
		},
		containers: []runtime.Container{
			{ID: "1", Image: "docker.io/library/nginx:1"},
			{ID: "2", Image: "docker.io/library/nginx:1"},
			{ID: "3", Image: "docker.io/library/redis:7"},
			{ID: "4", Image: "local/app:dev"},
			{ID: "5", Image: "docker.io/library/busybox@sha256:pinned"},
		},
		remote: map[string]string{
			"docker.io/library/nginx:1":  "sha256:new",
			"docker.io/library/redis:7":  "sha256:same",
			"docker.io/library/alpine:3": "sha256:a2",
		},
	}
	h := updatesRouter(t, rt)

	rec := send(t, h, http.MethodPost, "/api/v1/environments/local/image-updates/check", `{}`)
	var rep updates.Report
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &rep) != nil {
		t.Fatalf("check: %d %s", rec.Code, rec.Body)
	}
	// nginx, redis and the local image are used; busybox is pinned, alpine unused.
	if rep != (updates.Report{Checked: 3, Updates: 1, Failed: 1}) {
		t.Errorf("report %+v", rep)
	}

	var ov updates.Overview
	if rec := do(t, h, "/api/v1/environments/local/image-updates?namespace=default", &ov); rec.Code != http.StatusOK {
		t.Fatalf("overview: %d %s", rec.Code, rec.Body)
	}
	byRef := map[string]updates.Status{}
	for _, s := range ov.Images {
		byRef[s.Ref] = s
	}
	if s := byRef["docker.io/library/nginx:1"]; !s.UpdateAvailable || s.Containers != 2 || s.RemoteDigest != "sha256:new" {
		t.Errorf("nginx %+v", s)
	}
	if s := byRef["docker.io/library/redis:7"]; s.UpdateAvailable {
		t.Errorf("redis %+v", s)
	}
	if s := byRef["local/app:dev"]; s.UpdateAvailable || s.Error == "" {
		t.Errorf("local image %+v", s)
	}
	if _, ok := byRef["docker.io/library/alpine:3"]; ok {
		t.Error("unchecked alpine listed")
	}
	if ov.LastCheck == nil || ov.NextCheck != nil {
		t.Errorf("schedule %+v", ov)
	}

	// One reference on request, used or not; a pull (new local digest) clears the update.
	if rec := send(t, h, http.MethodPost, "/api/v1/environments/local/image-updates/check", `{"ref":"docker.io/library/alpine:3"}`); rec.Code != http.StatusOK {
		t.Fatalf("check one: %d %s", rec.Code, rec.Body)
	}
	rt.images[0].Digest = "sha256:new"
	do(t, h, "/api/v1/environments/local/image-updates?namespace=default", &ov)
	byRef = map[string]updates.Status{}
	for _, s := range ov.Images {
		byRef[s.Ref] = s
	}
	if !byRef["docker.io/library/alpine:3"].UpdateAvailable || byRef["docker.io/library/nginx:1"].UpdateAvailable {
		t.Errorf("after pull and single check: %+v", ov.Images)
	}

	if rec := send(t, h, http.MethodPost, "/api/v1/environments/local/image-updates/check", `{"ref":"docker.io/library/busybox@sha256:pinned"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("pinned ref: %d %s", rec.Code, rec.Body)
	}
}

func TestImageUpdatesDisabled(t *testing.T) {
	rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments/local/image-updates", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("%d", rec.Code)
	}
}
