// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/pessom/nerdui/internal/authz"
)

// signIn logs c in as user, changing a temporary password to newPass first when given.
func signIn(t *testing.T, c *client, user, pass, newPass string) SessionResponse {
	t.Helper()
	c.csrf = ""
	resp, body := c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"`+user+`","password":"`+pass+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d %s", user, resp.StatusCode, body)
	}
	var s SessionResponse
	_ = json.Unmarshal([]byte(body), &s)
	c.csrf = s.CSRFToken
	if newPass != "" {
		resp, body = c.do(http.MethodPost, "/api/v1/auth/password",
			`{"currentPassword":"`+pass+`","newPassword":"`+newPass+`"}`, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("change password of %s: %d %s", user, resp.StatusCode, body)
		}
		_ = json.Unmarshal([]byte(body), &s)
	}
	return s
}

func createUser(t *testing.T, c *client, name, role string) (int64, int) {
	t.Helper()
	resp, body := c.do(http.MethodPost, "/api/v1/users",
		`{"username":"`+name+`","password":"`+name+`'s first password","role":"`+role+`"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		return 0, resp.StatusCode
	}
	var r UserPasswordResponse
	_ = json.Unmarshal([]byte(body), &r)
	return r.User.ID, resp.StatusCode
}

func TestRolesHTTP(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	admin, _ := e.setupAdmin(t)
	c := newServer(t, e)
	if me := signIn(t, c, "admin", goodPassword, ""); !slices.Equal(me.Permissions, authz.All()[1:]) {
		t.Errorf("admin permissions = %v", me.Permissions)
	}

	resp, body := c.do(http.MethodGet, "/api/v1/roles", "", nil)
	var roles []Role
	_ = json.Unmarshal([]byte(body), &roles)
	if resp.StatusCode != http.StatusOK || len(roles) != 4 || roles[0].Name != "admin" || roles[0].Users != 1 || !roles[3].Builtin {
		t.Fatalf("roles: %d %s", resp.StatusCode, body)
	}
	for _, c2 := range []struct {
		body string
		want int
	}{
		{`{"name":"Bad Name","permissions":[]}`, http.StatusBadRequest},
		{`{"name":"viewer","permissions":[]}`, http.StatusConflict},
		{`{"name":"helpdesk","permissions":["users:read","users:write","containers:read","x:y"]}`, http.StatusCreated},
		{`{"name":"helpdesk","permissions":[]}`, http.StatusConflict},
	} {
		if resp, body := c.do(http.MethodPost, "/api/v1/roles", c2.body, nil); resp.StatusCode != c2.want {
			t.Errorf("create %s: %d %s", c2.body, resp.StatusCode, body)
		}
	}
	if resp, _ := c.do(http.MethodPut, "/api/v1/roles/viewer", `{"permissions":[]}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("change built-in: %d", resp.StatusCode)
	}

	carol, _ := createUser(t, c, "carol", "helpdesk")
	eve, _ := createUser(t, c, "eve", "admin")
	resp, body = c.do(http.MethodPost, "/api/v1/users", `{"username":"dave","password":"dave's first password"}`, nil)
	var dave UserPasswordResponse
	_ = json.Unmarshal([]byte(body), &dave)
	if resp.StatusCode != http.StatusCreated || dave.User.Role != "viewer" {
		t.Fatalf("default role: %d %s", resp.StatusCode, body)
	}
	if _, code := createUser(t, c, "ghost", "root"); code != http.StatusNotFound {
		t.Errorf("unknown role: %d", code)
	}

	// carol manages users, but never above her own permissions.
	h := newClientFor(t, c)
	me := signIn(t, h, "carol", "carol's first password", "carol's own password")
	if want := []authz.Action{authz.JobsRead, authz.ContainersRead, authz.UsersRead, authz.UsersWrite}; !slices.Equal(me.Permissions, want) {
		t.Errorf("carol permissions = %v", me.Permissions)
	}
	for _, role := range []string{"admin", "viewer"} {
		if _, code := createUser(t, h, "x"+role, role); code != http.StatusForbidden {
			t.Errorf("carol creates %s: %d", role, code)
		}
	}
	if resp, body := h.do(http.MethodPost, "/api/v1/roles", `{"name":"bigger","permissions":["images:read"]}`, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("carol creates a bigger role: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPost, "/api/v1/roles", `{"name":"reader","permissions":["containers:read"]}`, nil); resp.StatusCode != http.StatusCreated {
		t.Errorf("carol creates reader: %d %s", resp.StatusCode, body)
	}
	frank, code := createUser(t, h, "frank", "reader")
	if code != http.StatusCreated {
		t.Fatalf("carol creates frank: %d", code)
	}
	for _, id := range []int64{eve, dave.User.ID} {
		uid := strconv.FormatInt(id, 10)
		for _, req := range [][3]string{
			{http.MethodPost, "/api/v1/users/" + uid + "/password", `{}`},
			{http.MethodPatch, "/api/v1/users/" + uid, `{"disabled":true}`},
			{http.MethodDelete, "/api/v1/users/" + uid, ""},
			{http.MethodPost, "/api/v1/users/" + uid + "/mfa/reset", ""},
		} {
			if resp, body := h.do(req[0], req[1], req[2], nil); resp.StatusCode != http.StatusForbidden {
				t.Errorf("carol %s %s: %d %s", req[0], req[1], resp.StatusCode, body)
			}
		}
	}
	fid := strconv.FormatInt(frank, 10)
	if resp, body := h.do(http.MethodPost, "/api/v1/users/"+fid+"/password", `{}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("carol resets frank: %d %s", resp.StatusCode, body)
	}
	if resp, body := h.do(http.MethodPatch, "/api/v1/users/"+fid, `{"role":"admin"}`, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("carol makes frank admin: %d %s", resp.StatusCode, body)
	}

	// A role in use stays; a new role applies at the next request.
	if resp, _ := c.do(http.MethodDelete, "/api/v1/roles/helpdesk", "", nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete a role in use: %d", resp.StatusCode)
	}
	cid := strconv.FormatInt(carol, 10)
	if resp, body := c.do(http.MethodPatch, "/api/v1/users/"+cid, `{"role":"viewer"}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("carol to viewer: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do(http.MethodGet, "/api/v1/users", "", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("carol as viewer lists users: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/roles/helpdesk", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete helpdesk: %d", resp.StatusCode)
	}
	if resp, body := c.do(http.MethodPut, "/api/v1/roles/reader", `{"description":"Reads","permissions":["images:read"]}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("update reader: %d %s", resp.StatusCode, body)
	}
	if perms, _ := e.svc.RolePermissions(ctx, "reader"); !slices.Contains(perms, authz.ImagesRead) || slices.Contains(perms, authz.ContainersRead) {
		t.Errorf("reader after update = %v", perms)
	}

	// The last active administrator keeps the role.
	if resp, _ := c.do(http.MethodPatch, "/api/v1/users/"+strconv.FormatInt(admin.ID, 10), `{"role":"viewer"}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("own role: %d", resp.StatusCode)
	}
	if _, err := e.svc.SetRole(ctx, admin.ID, nil, eve, "editor"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetRole(ctx, eve, nil, admin.ID, "editor"); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("last admin: %v", err)
	}
}

func TestOIDCRoleDefault(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if c, err := e.svc.OIDCSettings(ctx); err != nil || c.Role != "admin" {
		t.Errorf("settings before roles: %+v %v", c, err)
	}
	c, err := e.svc.SetOIDCSettings(ctx, OIDCSettings{Issuer: "https://idp.example", ClientID: "nerdui"})
	if err != nil || c.Role != "viewer" {
		t.Errorf("new settings: %+v %v", c, err)
	}
}
