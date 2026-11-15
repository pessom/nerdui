// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestNetworks(t *testing.T) {
	rt := writable()
	rt.networks = []runtime.Network{
		{Name: "bridge", Driver: "bridge", Default: true, Containers: []runtime.NetworkContainer{}},
	}
	h := newRouter(rt)
	const base = "/api/v1/environments/local/networks"

	rec := send(t, h, http.MethodGet, base+"?namespace=k8s.io", "")
	var list struct {
		Namespace string
		Networks  []runtime.Network
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || list.Namespace != "k8s.io" || len(list.Networks) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = send(t, h, http.MethodPost, base, `{"name":"lan","driver":"macvlan","parent":"eth0","subnet":"192.168.1.0/24"}`)
	if rec.Code != http.StatusCreated || len(rt.netSpecs) != 1 || rt.netSpecs[0].Parent != "eth0" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPost, base, `{"name":"x","driver":"overlay"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad driver: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"/bridge", ""); rec.Code != http.StatusConflict {
		t.Errorf("remove default: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"/lan", ""); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"/lan", ""); rec.Code != http.StatusNotFound {
		t.Errorf("remove again: %d %s", rec.Code, rec.Body)
	}
	ro := newRouter(&fakeRuntime{})
	if rec := send(t, ro, http.MethodPost, base, `{"name":"x"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("read-only create: %d", rec.Code)
	}
}
