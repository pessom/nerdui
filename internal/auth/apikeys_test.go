// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
)

// keyServer adds a read route and a write route guarded like the real API.
func keyServer(t *testing.T, e *env) *client {
	t.Helper()
	g := authz.NewGuard(authz.AdminOnly{}, SubjectFromRequest, nil)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		e.svc.Routes(r, g)
		r.Group(func(r chi.Router) {
			r.Use(e.svc.RequireSession, CSRF, PasswordChanged)
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
			r.Method(http.MethodGet, "/things", g.Protect(authz.ContainersRead, ok))
			r.Method(http.MethodPost, "/things", g.Protect(authz.ContainersWrite, ok))
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func loginAdmin(t *testing.T, c *client) {
	t.Helper()
	c.csrf = ""
	resp, body := c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+goodPassword+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
	var s SessionResponse
	_ = json.Unmarshal([]byte(body), &s)
	c.csrf = s.CSRFToken
}

func TestAPIKeys(t *testing.T) {
	e := newEnv(t, true)
	c := keyServer(t, e)
	admin, _ := e.setupAdmin(t)
	loginAdmin(t, c)

	create := func(body string) CreatedAPIKey {
		t.Helper()
		resp, b := c.do(http.MethodPost, "/api/v1/auth/api-keys", body, nil)
		if resp.StatusCode != http.StatusCreated || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("create %s: %d %s", body, resp.StatusCode, b)
		}
		var k CreatedAPIKey
		_ = json.Unmarshal([]byte(b), &k)
		return k
	}
	full := create(`{"name":"ci"}`)
	ro := create(`{"name":"monitoring","readOnly":true,"expiresAt":"2027-03-02T12:00:00Z"}`)
	if !strings.HasPrefix(full.Key, APIKeyPrefix) || len(full.Key) < 40 || !strings.HasPrefix(full.Key, full.Prefix) || full.ReadOnly {
		t.Fatalf("created %+v", full)
	}
	if !ro.ReadOnly || ro.ExpiresAt == nil {
		t.Errorf("read-only key %+v", ro)
	}
	for _, bad := range []string{`{"name":""}`, `{"name":"x","expiresAt":"2020-01-01T00:00:00Z"}`, `{"name":"` + strings.Repeat("n", 65) + `"}`} {
		if resp, b := c.do(http.MethodPost, "/api/v1/auth/api-keys", bad, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("create %s: %d %s", bad, resp.StatusCode, b)
		}
	}

	// The list never contains the key.
	_, b := c.do(http.MethodGet, "/api/v1/auth/api-keys", "", nil)
	if strings.Contains(b, full.Key) || !strings.Contains(b, full.Prefix) || strings.Count(b, `"id"`) != 2 {
		t.Errorf("list %s", b)
	}

	// A key authenticates without cookies and without a CSRF token.
	bare := newClientFor(t, c)
	auth := func(k string) map[string]string { return map[string]string{"Authorization": "Bearer " + k} }
	if resp, b := bare.do(http.MethodPost, "/api/v1/things", "", auth(full.Key)); resp.StatusCode != http.StatusNoContent {
		t.Errorf("write with a key: %d %s", resp.StatusCode, b)
	}
	if resp, _ := bare.do(http.MethodGet, "/api/v1/auth/me", "", auth(full.Key)); resp.StatusCode != http.StatusOK {
		t.Errorf("me with a key: %d", resp.StatusCode)
	}
	// Read-only: reads only.
	if resp, _ := bare.do(http.MethodGet, "/api/v1/things", "", auth(ro.Key)); resp.StatusCode != http.StatusNoContent {
		t.Errorf("read with a read-only key: %d", resp.StatusCode)
	}
	if resp, b := bare.do(http.MethodPost, "/api/v1/things", "", auth(ro.Key)); resp.StatusCode != http.StatusForbidden || !strings.Contains(b, "read-only") {
		t.Errorf("write with a read-only key: %d %s", resp.StatusCode, b)
	}
	// Keys cannot manage keys, change the password or log out.
	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/auth/api-keys", ""},
		{http.MethodPost, "/api/v1/auth/api-keys", `{"name":"more"}`},
		{http.MethodPost, "/api/v1/auth/password", `{"currentPassword":"x","newPassword":"y"}`},
		{http.MethodPost, "/api/v1/auth/logout", ""},
	} {
		if resp, b := bare.do(req.method, req.path, req.body, auth(full.Key)); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with a key: %d %s", req.method, req.path, resp.StatusCode, b)
		}
	}
	// Last use is recorded.
	keys, _ := e.svc.ListAPIKeys(context.Background(), admin.ID)
	for _, k := range keys {
		if k.ID == full.ID && k.LastUsedAt == nil {
			t.Error("last use not recorded")
		}
	}

	// Unknown, expired and revoked keys are rejected.
	if resp, _ := bare.do(http.MethodGet, "/api/v1/things", "", auth(APIKeyPrefix+"nope")); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown key: %d", resp.StatusCode)
	}
	e.clock.Add(25 * time.Hour)
	if resp, _ := bare.do(http.MethodGet, "/api/v1/things", "", auth(ro.Key)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expired key: %d", resp.StatusCode)
	}
	loginAdmin(t, c) // the session went idle
	id := strconv.FormatInt(full.ID, 10)
	if resp, b := c.do(http.MethodDelete, "/api/v1/auth/api-keys/"+id, "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", resp.StatusCode, b)
	}
	if resp, _ := bare.do(http.MethodGet, "/api/v1/things", "", auth(full.Key)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked key: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/auth/api-keys/"+id, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revoke again: %d", resp.StatusCode)
	}
}

func TestAPIKeysOfOtherUsers(t *testing.T) {
	e := newEnv(t, true)
	admin, _ := e.setupAdmin(t)
	ctx := context.Background()
	bob, _, err := e.svc.CreateUser(ctx, "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	k, token, err := e.svc.CreateAPIKey(ctx, bob.ID, "bob's", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AuthenticateKey(ctx, token); err != nil {
		t.Fatalf("bob's key: %v", err)
	}
	// A disabled user's keys stop working; enabled again, they work.
	if _, err := e.svc.SetDisabled(ctx, admin.ID, bob.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AuthenticateKey(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("disabled user's key: %v", err)
	}
	if _, err := e.svc.SetDisabled(ctx, admin.ID, bob.ID, false); err != nil {
		t.Fatal(err)
	}

	c := keyServer(t, e)
	loginAdmin(t, c)
	bobPath := "/api/v1/users/" + strconv.FormatInt(bob.ID, 10) + "/api-keys"
	if resp, b := c.do(http.MethodGet, bobPath, "", nil); resp.StatusCode != http.StatusOK || !strings.Contains(b, "bob's") || strings.Contains(b, token) {
		t.Errorf("list bob's keys: %d %s", resp.StatusCode, b)
	}
	if resp, _ := c.do(http.MethodGet, "/api/v1/users/999/api-keys", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown user: %d", resp.StatusCode)
	}
	if resp, b := c.do(http.MethodDelete, bobPath+"/"+strconv.FormatInt(k.ID, 10), "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke bob's key: %d %s", resp.StatusCode, b)
	}
	if _, err := e.svc.AuthenticateKey(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("revoked: %v", err)
	}
	// Deleting a user deletes the keys.
	_, token2, _ := e.svc.CreateAPIKey(ctx, bob.ID, "again", true, nil)
	if err := e.svc.DeleteUser(ctx, admin.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AuthenticateKey(ctx, token2); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("deleted user's key: %v", err)
	}
}
