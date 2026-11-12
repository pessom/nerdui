// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/httpjson"
)

func TestCreateUserAndTemporaryPassword(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	admin, _ := e.setupAdmin(t)

	if _, _, err := e.svc.CreateUser(ctx, "bad name", ""); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("invalid username: %v", err)
	}
	if _, _, err := e.svc.CreateUser(ctx, "ADMIN", ""); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("duplicate (case-insensitive): %v", err)
	}
	if _, _, err := e.svc.CreateUser(ctx, "bob", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password: %v", err)
	}

	bob, generated, err := e.svc.CreateUser(ctx, "bob", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) < MinPasswordLen || !bob.MustChangePassword || bob.Disabled || bob.LastLoginAt != nil {
		t.Fatalf("created %+v with %q", bob, generated)
	}
	u, is, err := e.svc.Login(ctx, "bob", generated, Client{})
	if err != nil || !u.MustChangePassword {
		t.Fatalf("login with temporary password: %+v %v", u, err)
	}
	p, err := e.svc.Authenticate(ctx, is.Token)
	if err != nil || !p.User.MustChangePassword {
		t.Fatalf("principal %+v %v", p, err)
	}
	if d, _ := e.svc.GetUser(ctx, bob.ID); d.LastLoginAt == nil || !d.LastLoginAt.Equal(e.clock.Now().Truncate(time.Second)) {
		t.Errorf("last login %v", d.LastLoginAt)
	}

	// Own password change: the current one is verified, the flag is cleared and other
	// sessions are revoked.
	_, other, _ := e.svc.Login(ctx, "bob", generated, Client{})
	if err := e.svc.ChangePassword(ctx, p, "wrong password!", "bob's new password"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong current: %v", err)
	}
	if err := e.svc.ChangePassword(ctx, p, generated, generated); !errors.Is(err, ErrSamePassword) {
		t.Errorf("same password: %v", err)
	}
	if err := e.svc.ChangePassword(ctx, p, generated, "bob's new password"); err != nil {
		t.Fatal(err)
	}
	if p, err := e.svc.Authenticate(ctx, is.Token); err != nil || p.User.MustChangePassword {
		t.Errorf("current session after change: %+v %v", p, err)
	}
	if _, err := e.svc.Authenticate(ctx, other.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Error("other session not revoked")
	}

	// An administrator sets a temporary password: sessions end, a change is required again.
	if _, err := e.svc.SetTemporaryPassword(ctx, admin.ID, admin.ID, ""); !errors.Is(err, ErrOwnAccount) {
		t.Errorf("own password via admin endpoint: %v", err)
	}
	if _, err := e.svc.SetTemporaryPassword(ctx, admin.ID, 999, ""); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if g, err := e.svc.SetTemporaryPassword(ctx, admin.ID, bob.ID, "chosen by the admin"); err != nil || g != "" {
		t.Fatalf("set password: %q %v", g, err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Error("sessions survived a password reset")
	}
	if u, _, err := e.svc.Login(ctx, "bob", "chosen by the admin", Client{}); err != nil || !u.MustChangePassword {
		t.Errorf("login after reset: %+v %v", u, err)
	}
}

func TestChangePasswordRateLimited(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	_, is := e.setupAdmin(t)
	p, _ := e.svc.Authenticate(ctx, is.Token)
	var err error
	for range 10 {
		if err = e.svc.ChangePassword(ctx, p, "wrong password!", "a brand new password"); err != nil && !errors.Is(err, ErrWrongPassword) {
			break
		}
	}
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("no rate limit: %v", err)
	}
}

func TestDisableAndDeleteUsers(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	admin, _ := e.setupAdmin(t)

	// The only active admin cannot be disabled or deleted, also not by themselves.
	if _, err := e.svc.SetDisabled(ctx, admin.ID, admin.ID, true); !errors.Is(err, ErrOwnAccount) {
		t.Errorf("disable self: %v", err)
	}
	if err := e.svc.DeleteUser(ctx, admin.ID, admin.ID); !errors.Is(err, ErrOwnAccount) {
		t.Errorf("delete self: %v", err)
	}

	bob, _, _ := e.svc.CreateUser(ctx, "bob", "bob's first password")
	_, is, err := e.svc.Login(ctx, "bob", "bob's first password", Client{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.SetDisabled(ctx, admin.ID, bob.ID, true)
	if err != nil || !d.Disabled {
		t.Fatalf("disable: %+v %v", d, err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Error("session of a disabled user still valid")
	}
	if _, _, err := e.svc.Login(ctx, "bob", "bob's first password", Client{}); !errors.Is(err, ErrUserDisabled) {
		t.Errorf("login while disabled: %v", err)
	}
	// A wrong password does not reveal that the account is disabled.
	if _, _, err := e.svc.Login(ctx, "bob", "not the password", Client{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password while disabled: %v", err)
	}

	// bob is the last active admin besides the caller only while enabled.
	if _, err := e.svc.SetDisabled(ctx, bob.ID, admin.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last active admin: %v", err)
	}
	if err := e.svc.DeleteUser(ctx, bob.ID, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last active admin: %v", err)
	}

	if d, err := e.svc.SetDisabled(ctx, admin.ID, bob.ID, false); err != nil || d.Disabled {
		t.Fatalf("enable: %+v %v", d, err)
	}
	if _, _, err := e.svc.Login(ctx, "bob", "bob's first password", Client{}); err != nil {
		t.Errorf("login after enable: %v", err)
	}
	if err := e.svc.DeleteUser(ctx, admin.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetUser(ctx, bob.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("deleted user: %v", err)
	}
	if err := e.svc.DeleteUser(ctx, admin.ID, bob.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("delete twice: %v", err)
	}
	us, _ := e.svc.ListUsers(ctx)
	if len(us) != 1 || us[0].Username != "admin" {
		t.Errorf("users %+v", us)
	}
}

func TestResetPasswordEnablesAccount(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	admin, _ := e.setupAdmin(t)
	bob, _, _ := e.svc.CreateUser(ctx, "bob", "bob's first password")
	if _, err := e.svc.SetDisabled(ctx, admin.ID, bob.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := ResetPassword(ctx, e.st, "bob", "recovered password", time.Now()); err != nil {
		t.Fatal(err)
	}
	u, _, err := e.svc.Login(ctx, "bob", "recovered password", Client{})
	if err != nil || u.MustChangePassword {
		t.Errorf("after CLI reset: %+v %v", u, err)
	}
}

func TestUsersHTTP(t *testing.T) {
	e := newEnv(t, true)
	c := newServer(t, e)
	admin, _ := e.setupAdmin(t)
	login := func(c *client, user, pass string) SessionResponse {
		t.Helper()
		c.csrf = ""
		resp, body := c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"`+user+`","password":"`+pass+`"}`, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login %s: %d %s", user, resp.StatusCode, body)
		}
		var s SessionResponse
		_ = json.Unmarshal([]byte(body), &s)
		c.csrf = s.CSRFToken
		return s
	}
	login(c, "admin", goodPassword)

	resp, body := c.do(http.MethodPost, "/api/v1/users", `{"username":"bob"}`, nil)
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var created UserPasswordResponse
	_ = json.Unmarshal([]byte(body), &created)
	if created.User == nil || !created.User.MustChangePassword || created.GeneratedPassword == "" {
		t.Fatalf("create response %s", body)
	}
	bobID := strconv.FormatInt(created.User.ID, 10)
	if resp, body := c.do(http.MethodPost, "/api/v1/users", `{"username":"bob"}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate: %d %s", resp.StatusCode, body)
	}
	if resp, body := c.do(http.MethodGet, "/api/v1/users", "", nil); resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, `"username":"bob"`) || strings.Contains(body, "password_hash") || strings.Contains(body, "$argon2") {
		t.Errorf("list: %d %s", resp.StatusCode, body)
	}
	if resp, _ := c.do(http.MethodGet, "/api/v1/users/x", "", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad id: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodGet, "/api/v1/users/999", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: %d", resp.StatusCode)
	}
	self := strconv.FormatInt(admin.ID, 10)
	if resp, body := c.do(http.MethodPatch, "/api/v1/users/"+self, `{"disabled":true}`, nil); resp.StatusCode != http.StatusConflict || errCode(t, body) != httpjson.CodeConflict {
		t.Errorf("disable self: %d %s", resp.StatusCode, body)
	}
	if resp, _ := c.do(http.MethodPatch, "/api/v1/users/"+bobID, `{}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty patch: %d", resp.StatusCode)
	}

	// bob signs in with the temporary password: everything but the own session is blocked.
	b := newClientFor(t, c)
	login(b, "bob", created.GeneratedPassword)
	if resp, body := b.do(http.MethodGet, "/api/v1/users", "", nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != httpjson.CodePasswordChangeRequired {
		t.Errorf("users before change: %d %s", resp.StatusCode, body)
	}
	if resp, body := b.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != httpjson.CodePasswordChangeRequired {
		t.Errorf("api before change: %d %s", resp.StatusCode, body)
	}
	if resp, body := b.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"mustChangePassword":true`) {
		t.Errorf("me before change: %d %s", resp.StatusCode, body)
	}
	change := `{"currentPassword":"` + created.GeneratedPassword + `","newPassword":"bob's own password"}`
	if resp, body := b.do(http.MethodPost, "/api/v1/auth/password", `{"currentPassword":"nope nope nope","newPassword":"bob's own password"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong current password: %d %s", resp.StatusCode, body)
	}
	if resp, body := b.do(http.MethodPost, "/api/v1/auth/password", change, nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"mustChangePassword":false`) {
		t.Fatalf("change: %d %s", resp.StatusCode, body)
	}
	if resp, _ := b.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("api after change: %d", resp.StatusCode)
	}

	// Reset by the admin: bob's session ends; the generated password is returned once.
	resp, body = c.do(http.MethodPost, "/api/v1/users/"+bobID+"/password", `{}`, nil)
	var reset UserPasswordResponse
	_ = json.Unmarshal([]byte(body), &reset)
	if resp.StatusCode != http.StatusOK || reset.GeneratedPassword == "" {
		t.Fatalf("reset: %d %s", resp.StatusCode, body)
	}
	if resp, _ := b.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bob after reset: %d", resp.StatusCode)
	}

	if resp, body := c.do(http.MethodPatch, "/api/v1/users/"+bobID, `{"disabled":true}`, nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"disabled":true`) {
		t.Errorf("disable: %d %s", resp.StatusCode, body)
	}
	b.csrf = ""
	if resp, body := b.do(http.MethodPost, "/api/v1/auth/login", `{"username":"bob","password":"`+reset.GeneratedPassword+`"}`, nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != httpjson.CodeForbidden {
		t.Errorf("login of disabled bob: %d %s", resp.StatusCode, body)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/users/"+bobID, "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/users/"+bobID, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("delete twice: %d", resp.StatusCode)
	}
}

// newClientFor returns a client with its own cookie jar for the same server.
func newClientFor(t *testing.T, c *client) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: c.base, http: &http.Client{Jar: jar}}
}
