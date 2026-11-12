// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestRegistries(t *testing.T) {
	f := &fakeRuntime{nerdctl: runtime.NerdctlStatus{Compatible: true}, imageErr: map[string]error{
		"bad.example": fmt.Errorf("%w: unauthorized", errdefs.ErrInvalidArgument),
	}}
	h := newRouter(f)
	base := "/api/v1/environments/local/registries"

	rec := send(t, h, http.MethodPost, base, `{"server":"reg.local:5000","username":"bob","password":"secret"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"warning":"stored unencrypted"`) {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("password echoed: %s", rec.Body)
	}
	if rec := send(t, h, http.MethodPost, base, `{"server":"bad.example","username":"x","password":"y"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("rejected login: %d %s", rec.Code, rec.Body)
	}

	rec = send(t, h, http.MethodGet, base, "")
	var got struct{ Registries []runtime.Registry }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || len(got.Registries) != 1 || got.Registries[0].Username != "bob" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	if rec := send(t, h, http.MethodDelete, base+"?server=reg.local:5000", ""); rec.Code != http.StatusNoContent {
		t.Errorf("logout: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, base+"?server=reg.local:5000", ""); rec.Code != http.StatusNotFound {
		t.Errorf("logout twice: %d %s", rec.Code, rec.Body)
	}
}
