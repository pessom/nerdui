// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B (SHA-1), last six digits.
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(secret, totpStep(time.Unix(ts, 0))); got != want {
			t.Errorf("t=%d: %s, want %s", ts, got, want)
		}
	}
	b := b32.EncodeToString(secret)
	now := time.Unix(1111111109, 0)
	if _, ok := verifyTOTP(b, "081 804", now); !ok {
		t.Error("code with a space rejected")
	}
	if _, ok := verifyTOTP(b, "081804", now.Add(15*time.Second)); !ok {
		t.Error("previous step rejected")
	}
	if _, ok := verifyTOTP(b, "081804", now.Add(95*time.Second)); ok {
		t.Error("old code accepted")
	}
	if u := totpURI("nerdui", "ad min", "ABC"); !strings.HasPrefix(u, "otpauth://totp/nerdui:ad%20min?") || !strings.Contains(u, "secret=ABC") {
		t.Errorf("uri %s", u)
	}
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return v
}

func TestMFAFlow(t *testing.T) {
	e := newEnv(t, true)
	c := newServer(t, e)
	resp, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"token":"`+e.setupToken(t)+`","username":"admin","password":"`+goodPassword+`"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	c.csrf = decode[SessionResponse](t, body).CSRFToken

	if resp, _ := c.do(http.MethodPost, "/api/v1/auth/mfa/setup", `{"password":"wrong-password"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("setup with a wrong password: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/auth/mfa/enable", `{"code":"123456"}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("enable before setup: %d", resp.StatusCode)
	}
	resp, body = c.do(http.MethodPost, "/api/v1/auth/mfa/setup", `{"password":"`+goodPassword+`"}`, nil)
	enr := decode[MFAEnrollment](t, body)
	if resp.StatusCode != http.StatusOK || enr.Secret == "" || !strings.Contains(enr.URI, "otpauth://totp/nerdui:admin") {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	secret, _ := b32.DecodeString(enr.Secret)
	code := func() string { return totpCode(secret, totpStep(e.clock.Now())) }
	if resp, _ := c.do(http.MethodPost, "/api/v1/auth/mfa/enable", `{"code":"000000"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("enable with a wrong code: %d", resp.StatusCode)
	}
	resp, body = c.do(http.MethodPost, "/api/v1/auth/mfa/enable", `{"code":"`+code()+`"}`, nil)
	backup := decode[map[string][]string](t, body)["backupCodes"]
	if resp.StatusCode != http.StatusOK || len(backup) != backupCodeCount {
		t.Fatalf("enable: %d %s", resp.StatusCode, body)
	}

	// Signing in now takes two steps.
	login := func() (*client, string) {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		lc := &client{t: t, base: c.base, http: &http.Client{Jar: jar}}
		resp, body := lc.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+goodPassword+`"}`, nil)
		ch := decode[MFAChallengeResponse](t, body)
		if resp.StatusCode != http.StatusOK || !ch.MFARequired || ch.MFAToken == "" || len(resp.Cookies()) != 0 {
			t.Fatalf("login: %d %s", resp.StatusCode, body)
		}
		return lc, ch.MFAToken
	}
	second := func(lc *client, token, code string) int {
		t.Helper()
		resp, _ := lc.do(http.MethodPost, "/api/v1/auth/login/mfa", `{"mfaToken":"`+token+`","code":"`+code+`"}`, nil)
		return resp.StatusCode
	}
	lc, token := login()
	if got := second(lc, token, "000000"); got != http.StatusUnauthorized {
		t.Errorf("wrong code: %d", got)
	}
	if got := second(lc, token, code()); got != http.StatusUnauthorized {
		t.Errorf("the code used to enable was accepted again: %d", got)
	}
	e.clock.Add(30 * time.Second)
	if got := second(lc, "forged", code()); got != http.StatusUnauthorized {
		t.Errorf("forged token: %d", got)
	}
	if got := second(lc, token, code()); got != http.StatusOK {
		t.Fatalf("right code: %d", got)
	}
	if resp, body := lc.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusOK || !decode[SessionResponse](t, body).User.MFAEnabled {
		t.Errorf("me: %d %s", resp.StatusCode, body)
	}
	lc, token = login()
	if got := second(lc, token, code()); got != http.StatusUnauthorized {
		t.Errorf("replayed code: %d", got)
	}
	if got := second(lc, token, strings.ToUpper(backup[0])); got != http.StatusOK {
		t.Errorf("backup code: %d", got)
	}
	lc, token = login()
	if got := second(lc, token, backup[0]); got != http.StatusUnauthorized {
		t.Errorf("used backup code: %d", got)
	}
	resp, body = c.do(http.MethodGet, "/api/v1/auth/mfa", "", nil)
	if st := decode[MFAStatus](t, body); resp.StatusCode != http.StatusOK || !st.Enabled || st.BackupCodesLeft != backupCodeCount-1 {
		t.Errorf("status %s", body)
	}

	// Required for everyone: a user without it may only set it up.
	ctx := context.Background()
	if err := e.svc.SetRequireMFA(ctx, true); err != nil {
		t.Fatal(err)
	}
	bob, _, err := e.svc.CreateUser(ctx, "bob", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	bc := &client{t: t, base: c.base, http: &http.Client{Jar: jar}}
	resp, body = bc.do(http.MethodPost, "/api/v1/auth/login", `{"username":"bob","password":"`+goodPassword+`"}`, nil)
	sr := decode[SessionResponse](t, body)
	if resp.StatusCode != http.StatusOK || !sr.MFASetupRequired {
		t.Fatalf("bob login: %d %s", resp.StatusCode, body)
	}
	bc.csrf = sr.CSRFToken
	// An administrator chose bob's password: he changes it first, then sets up MFA.
	if resp, body := bc.do(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+goodPassword+`","newPassword":"another-good-password-9"}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("bob password: %d %s", resp.StatusCode, body)
	}
	if resp, body := bc.do(http.MethodGet, "/api/v1/users", "", nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != CodeMFASetupRequired {
		t.Errorf("users before setup: %d %s", resp.StatusCode, body)
	}
	if resp, _ := bc.do(http.MethodGet, "/api/v1/auth/mfa", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("mfa status before setup: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodGet, "/api/v1/settings/security", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("settings: %d", resp.StatusCode)
	}

	// An administrator resets another user's second factor.
	if err := e.svc.ResetMFA(ctx, bob.ID, bob.ID); err == nil {
		t.Error("reset own")
	}
	if resp, body := c.do(http.MethodPost, "/api/v1/users/"+jsonID(bob.ID)+"/mfa/reset", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("reset: %d %s", resp.StatusCode, body)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/auth/mfa/disable", `{"password":"`+goodPassword+`"}`, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("disable: %d", resp.StatusCode)
	}
	if resp, body := c.do(http.MethodGet, "/api/v1/users", "", nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != CodeMFASetupRequired {
		t.Errorf("admin without MFA while required: %d %s", resp.StatusCode, body)
	}
}

func jsonID(id int64) string { b, _ := json.Marshal(id); return string(b) }

func TestResetMFAOffline(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	u, is := e.setupAdmin(t)
	enr, err := e.svc.StartMFA(ctx, u.ID, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := b32.DecodeString(enr.Secret)
	if _, err := e.svc.EnableMFA(ctx, u.ID, totpCode(secret, totpStep(e.clock.Now()))); err != nil {
		t.Fatal(err)
	}
	var ch *MFAChallengeError
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{}); !errors.As(err, &ch) {
		t.Fatalf("login with MFA: %v", err)
	}
	if err := ResetMFAOffline(ctx, e.st, "nobody", time.Now()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if err := ResetMFAOffline(ctx, e.st, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Error("sessions not revoked")
	}
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{}); err != nil {
		t.Errorf("login after reset: %v", err)
	}
}
