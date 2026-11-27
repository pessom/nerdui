// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/proxy"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newServer(t *testing.T, e *env) *client {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		e.svc.Routes(r, authz.NewGuard(authz.Roles{Lookup: e.svc}, SubjectFromRequest, nil))
		r.Group(func(r chi.Router) {
			r.Use(e.svc.RequireSession, CSRF, PasswordChanged)
			r.Post("/protected", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string, hdr map[string]string) (*http.Response, string) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body)) //nolint:noctx // test
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(CSRFHeader, c.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func errCode(t *testing.T, body string) string {
	t.Helper()
	var e httpjson.ErrorBody
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("not an error envelope: %q", body)
	}
	return e.Error.Code
}

func TestHTTPFlow(t *testing.T) {
	e := newEnv(t, true)
	c := newServer(t, e)
	tok := e.setupToken(t)

	resp, body := c.do(http.MethodGet, "/api/v1/setup", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"required":true`) {
		t.Fatalf("setup status: %d %s", resp.StatusCode, body)
	}

	// Unauthenticated access to protected routes.
	if resp, body := c.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusUnauthorized || errCode(t, body) != httpjson.CodeUnauthenticated {
		t.Fatalf("me without session: %d %s", resp.StatusCode, body)
	}

	// Setup: content type, origin and token are enforced.
	setupBody := `{"token":"` + tok + `","username":"admin","password":"` + goodPassword + `"}`
	if resp, _ := c.do(http.MethodPost, "/api/v1/setup", setupBody, map[string]string{"Content-Type": "text/plain"}); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain setup: %d", resp.StatusCode)
	}
	if resp, body := c.do(http.MethodPost, "/api/v1/setup", setupBody, map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != http.StatusForbidden || errCode(t, body) != httpjson.CodeCSRF {
		t.Errorf("cross-origin setup: %d %s", resp.StatusCode, body)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/setup", `{"token":"x","username":"admin","password":"`+goodPassword+`"}`, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/setup", `{"token":"x","extra":1}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: %d", resp.StatusCode)
	}
	resp, body = c.do(http.MethodPost, "/api/v1/setup", setupBody, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	cookie := resp.Cookies()[0]
	if cookie.Name != CookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Errorf("cookie attributes: %+v", cookie)
	}
	var sess SessionResponse
	_ = json.Unmarshal([]byte(body), &sess)
	if sess.User.Username != "admin" || sess.CSRFToken == "" {
		t.Fatalf("session response: %s", body)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/setup", setupBody, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("repeated setup: %d", resp.StatusCode)
	}

	// Authenticated: me works, unsafe requests need the CSRF token and same origin.
	if resp, body := c.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, sess.CSRFToken) {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
	if resp, body := c.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusForbidden || errCode(t, body) != httpjson.CodeCSRF {
		t.Errorf("missing CSRF: %d %s", resp.StatusCode, body)
	}
	c.csrf = "wrong"
	if resp, _ := c.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong CSRF: %d", resp.StatusCode)
	}
	c.csrf = sess.CSRFToken
	if resp, _ := c.do(http.MethodPost, "/api/v1/protected", "", map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin with CSRF token: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("valid CSRF: %d", resp.StatusCode)
	}

	// Logout revokes the session and clears the cookie.
	resp, _ = c.do(http.MethodPost, "/api/v1/auth/logout", "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := c.do(http.MethodGet, "/api/v1/auth/me", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("me after logout: %d", resp.StatusCode)
	}

	// Login and rate limiting.
	c.csrf = ""
	loginBody := func(p string) string { return `{"username":"admin","password":"` + p + `"}` }
	if resp, body := c.do(http.MethodPost, "/api/v1/auth/login", loginBody("wrong password"), nil); resp.StatusCode != http.StatusUnauthorized || errCode(t, body) != httpjson.CodeUnauthenticated {
		t.Errorf("wrong password: %d %s", resp.StatusCode, body)
	}
	if resp, body := c.do(http.MethodPost, "/api/v1/auth/login", loginBody(goodPassword), nil); resp.StatusCode != http.StatusOK || !strings.Contains(body, "csrfToken") {
		t.Fatalf("login: %d %s", resp.StatusCode, body)
	}
	for i := 0; i < 5; i++ {
		c.do(http.MethodPost, "/api/v1/auth/login", loginBody("wrong password"), nil)
	}
	resp, body = c.do(http.MethodPost, "/api/v1/auth/login", loginBody(goodPassword), nil)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" || errCode(t, body) != httpjson.CodeRateLimited {
		t.Errorf("rate limit: %d %v %s", resp.StatusCode, resp.Header, body)
	}
}

func TestInsecureCookieWithoutTLS(t *testing.T) {
	e := newEnv(t, false)
	c := newServer(t, e)
	resp, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"token":"`+e.setupToken(t)+`","username":"admin","password":"`+goodPassword+`"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	if resp.Cookies()[0].Secure {
		t.Error("Secure cookie set although TLS is disabled")
	}
}

// Behind a trusted proxy that terminates TLS, the cookie is Secure even with --tls=false (#112).
func TestSecureCookieBehindProxy(t *testing.T) {
	e := newEnv(t, false)
	trusted, _ := proxy.ParsePrefixes([]string{"127.0.0.1", "::1"})
	r := chi.NewRouter()
	r.Use(proxy.Middleware(trusted))
	r.Route("/api/v1", func(r chi.Router) {
		e.svc.Routes(r, authz.NewGuard(authz.Roles{Lookup: e.svc}, SubjectFromRequest, nil))
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	c := &client{t: t, base: srv.URL, http: srv.Client()}
	resp, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"token":"`+e.setupToken(t)+`","username":"admin","password":"`+goodPassword+`"}`,
		map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-For": "203.0.113.7"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	if !resp.Cookies()[0].Secure {
		t.Error("cookie not Secure behind an HTTPS proxy")
	}
}

func TestExpiredCookieIsCleared(t *testing.T) {
	e := newEnv(t, true)
	c := newServer(t, e)
	e.setupAdmin(t)
	resp, _ := c.do(http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Cookie": CookieName + "=bogus"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cleared := false
	for _, ck := range resp.Cookies() {
		if ck.Name == CookieName && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("invalid session cookie not cleared")
	}
}

type authEvent struct {
	action, user string
	ok           bool
}

type fakeAuthAudit struct{ got []authEvent }

func (f *fakeAuthAudit) Auth(_ *http.Request, action, username string, ok bool, _ string) {
	f.got = append(f.got, authEvent{action, username, ok})
}

func TestAuthAudit(t *testing.T) {
	e := newEnv(t, true)
	a := &fakeAuthAudit{}
	e.svc.opts.Audit = a
	c := newServer(t, e)
	if resp, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"token":"`+e.setupToken(t)+`","username":"admin","password":"`+goodPassword+`"}`, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong-password-1"}`, nil)
	c.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+goodPassword+`"}`, nil)
	want := []authEvent{{"auth:setup", "admin", true}, {"auth:login", "admin", false}, {"auth:login", "admin", true}}
	if len(a.got) != len(want) {
		t.Fatalf("events %+v", a.got)
	}
	for i := range want {
		if a.got[i] != want[i] {
			t.Errorf("event %d: %+v, want %+v", i, a.got[i], want[i])
		}
	}
}

func TestCookiePath(t *testing.T) {
	for path, want := range map[string]string{"": "/", "/nerdui": "/nerdui"} {
		s := &Service{opts: Options{CookiePath: path}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		s.setCookie(rec, req, Issued{Token: "t", ExpiresAt: time.Now().Add(time.Hour)})
		s.clearCookie(rec, req)
		for _, c := range rec.Result().Cookies() {
			if c.Path != want {
				t.Errorf("CookiePath %q: cookie path %q", path, c.Path)
			}
		}
	}
}
