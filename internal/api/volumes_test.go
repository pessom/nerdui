// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestVolumes(t *testing.T) {
	rt := writable()
	rt.volumes = []runtime.Volume{
		{Name: "pgdata", Containers: []runtime.VolumeMounter{{ID: "c1", Name: "db"}}},
		{Name: "old", Containers: []runtime.VolumeMounter{}},
	}
	h := newRouter(rt)
	const base = "/api/v1/environments/local/volumes"

	rec := send(t, h, http.MethodGet, base+"?namespace=k8s.io&size=true", "")
	var list struct {
		Namespace string
		Volumes   []runtime.Volume
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || list.Namespace != "k8s.io" || len(list.Volumes) != 2 || !rt.volumeSize {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodGet, base+"?size=maybe", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad size: %d", rec.Code)
	}

	rec = send(t, h, http.MethodPost, base, `{"name":"cache","labels":{"app":"web"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPost, base, `{"name":"cache"}`); rec.Code != http.StatusConflict {
		t.Errorf("create existing: %d %s", rec.Code, rec.Body)
	}

	if rec := send(t, h, http.MethodDelete, base+"/pgdata", ""); rec.Code != http.StatusConflict {
		t.Errorf("remove used: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("remove missing: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"/old", ""); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}

	// Mutations need a writable nerdctl (ADR-0011).
	ro := newRouter(&fakeRuntime{})
	if rec := send(t, ro, http.MethodPost, base, `{"name":"x"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("read-only create: %d", rec.Code)
	}
}
