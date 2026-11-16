// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPreferences(t *testing.T) {
	e := newEnv(t, true)
	c := keyServer(t, e)
	e.setupAdmin(t)
	loginAdmin(t, c)

	resp, body := c.do(http.MethodGet, "/api/v1/auth/preferences", "", nil)
	var p Preferences
	_ = json.Unmarshal([]byte(body), &p)
	if resp.StatusCode != http.StatusOK || p.Theme != "auto" || p.DateFormat != "relative" || p.RefreshSeconds != 0 || p.HiddenColumns == nil {
		t.Fatalf("defaults: %d %s", resp.StatusCode, body)
	}
	good := `{"theme":"dark","dateFormat":"iso","refreshSeconds":30,"hiddenColumns":{"containers":["ports","createdAt"]}}`
	if resp, b := c.do(http.MethodPut, "/api/v1/auth/preferences", good, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d %s", resp.StatusCode, b)
	}
	_, body = c.do(http.MethodGet, "/api/v1/auth/preferences", "", nil)
	_ = json.Unmarshal([]byte(body), &p)
	if p.Theme != "dark" || p.DateFormat != "iso" || p.RefreshSeconds != 30 || strings.Join(p.HiddenColumns["containers"], ",") != "ports,createdAt" {
		t.Errorf("saved %+v", p)
	}
	for _, bad := range []string{
		`{"theme":"pink","dateFormat":"iso","refreshSeconds":0}`,
		`{"theme":"auto","dateFormat":"unix","refreshSeconds":0}`,
		`{"theme":"auto","dateFormat":"iso","refreshSeconds":7}`,
		`{"theme":"auto","dateFormat":"iso","refreshSeconds":0,"hiddenColumns":{"x":["a b"]}}`,
		`{"theme":"auto","dateFormat":"iso","refreshSeconds":0,"extra":1}`,
	} {
		if resp, b := c.do(http.MethodPut, "/api/v1/auth/preferences", bad, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, resp.StatusCode, b)
		}
	}
	// API keys may read but not change preferences.
	_, token, _ := e.svc.CreateAPIKey(t.Context(), 1, "k", false, nil)
	bare := newClientFor(t, c)
	auth := map[string]string{"Authorization": "Bearer " + token}
	if resp, _ := bare.do(http.MethodGet, "/api/v1/auth/preferences", "", auth); resp.StatusCode != http.StatusOK {
		t.Errorf("get with a key: %d", resp.StatusCode)
	}
	if resp, _ := bare.do(http.MethodPut, "/api/v1/auth/preferences", good, auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("put with a key: %d", resp.StatusCode)
	}
}
