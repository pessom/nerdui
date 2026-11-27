// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"cmp"
	"errors"
	"html"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// oidcStateCookie keeps the state of a sign-in in progress. It is SameSite=Lax: the provider
// sends the browser back with a cross-site navigation, on which a Strict cookie is not sent.
const oidcStateCookie = "nerdui_oidc_state"

// OIDCStatus tells the sign-in page whether to show the single sign-on button. Keep in sync
// with api/openapi.yaml.
type OIDCStatus struct {
	Enabled     bool   `json:"enabled"`
	ButtonLabel string `json:"buttonLabel"`
}

// OIDCSettingsResponse is the settings with the callback URL to register at the provider.
// Keep in sync with api/openapi.yaml.
type OIDCSettingsResponse struct {
	OIDCSettings
	CallbackURL string `json:"callbackUrl"`
}

// oidcPublicRoutes are used before signing in.
func (s *Service) oidcPublicRoutes(r chi.Router) {
	r.Method(http.MethodGet, "/auth/oidc", authz.Public(s.handleOIDCStatus))
	r.Method(http.MethodGet, "/auth/oidc/login", authz.Public(s.handleOIDCLogin))
	r.Method(http.MethodGet, "/auth/oidc/callback", authz.Public(s.handleOIDCCallback))
}

// oidcAdminRoutes configure single sign-on and unlink users.
func (s *Service) oidcAdminRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/settings/oidc", g.Protect(authz.SettingsRead, s.handleGetOIDC))
	r.Method(http.MethodPut, "/settings/oidc", g.Protect(authz.SettingsWrite, s.handlePutOIDC))
	r.Method(http.MethodDelete, "/users/{id}/oidc", g.Protect(authz.UsersWrite, s.handleUnlinkOIDC))
}

func (s *Service) handleOIDCStatus(w http.ResponseWriter, r *http.Request) {
	c, err := s.OIDCSettings(r.Context())
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, OIDCStatus{Enabled: c.Enabled, ButtonLabel: c.withDefaults().ButtonLabel})
}

// appURL is a path of the web UI under the base path.
func (s *Service) appURL(p string) string {
	return path.Join(s.cookiePath(), p)
}

// callbackURL is the callback derived from the request: the scheme, the host (from a trusted
// proxy, #112) and the base path.
func (s *Service) callbackURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || s.secure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + s.appURL("/api/v1/auth/oidc/callback")
}

// safeRedirect keeps only paths of the web UI: "/…", not "//host" or a URL.
func safeRedirect(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") || strings.ContainsAny(p, "\r\n") {
		return "/"
	}
	return p
}

// handleOIDCLogin sends the browser to the provider:
//
//	GET /api/v1/auth/oidc/login?redirect=/path
func (s *Service) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	redirect := safeRedirect(r.URL.Query().Get("redirect"))
	authURL, state, err := s.OIDCStart(r.Context(), redirect, s.callbackURL(r))
	if err != nil {
		s.opts.Logger.Warn("single sign-on cannot start", "error", err)
		s.oidcFail(w, r, redirect, err)
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name: oidcStateCookie, Value: state, Path: s.cookiePath(), MaxAge: int(oidcStateTTL.Seconds()),
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
	// The URL is the authorization endpoint from the discovery document of the configured issuer.
	http.Redirect(w, r, authURL, http.StatusFound) //nolint:gosec // G710: not from the request
}

// handleOIDCCallback completes the sign-in when the provider sends the browser back:
//
//	GET /api/v1/auth/oidc/callback?code=&state=
func (s *Service) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookieState := ""
	if c, err := r.Cookie(oidcStateCookie); err == nil {
		cookieState = c.Value
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name: oidcStateCookie, Path: s.cookiePath(), MaxAge: -1,
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
	if e := q.Get("error"); e != "" {
		msg := e
		if d := q.Get("error_description"); d != "" {
			msg += ": " + d
		}
		s.oidcAudit(r, "", false, "the provider refused: "+msg)
		s.oidcFail(w, r, "/", errors.New("the provider refused the sign-in: "+msg))
		return
	}
	u, issued, redirect, id, err := s.OIDCFinish(r.Context(), cookieState, q.Get("state"), q.Get("code"), clientOf(r))
	name := u.Username
	if name == "" {
		name = id.Username
	}
	if err != nil {
		s.oidcAudit(r, name, false, err.Error())
		s.opts.Logger.Warn("single sign-on failed", "username", name, "identity", id.ID, "error", err)
		s.oidcFail(w, r, redirect, err)
		return
	}
	s.oidcAudit(r, name, true, "via single sign-on")
	s.setCookie(w, r, issued)
	// The session cookie is SameSite=Strict, and this request comes from the provider: a
	// redirect would carry on the cross-site navigation without the cookie. A page that
	// navigates on its own starts a same-site one.
	continueTo(w, s.appURL(redirect))
}

func (s *Service) oidcAudit(r *http.Request, username string, ok bool, msg string) {
	if s.opts.Audit != nil {
		s.opts.Audit.Auth(r, "auth:login-sso", truncate(username, 100), ok, msg)
	}
}

// oidcFail returns to the sign-in page with the reason.
func (s *Service) oidcFail(w http.ResponseWriter, _ *http.Request, redirect string, err error) {
	msg := err.Error()
	var rl *RateLimitedError
	if !errors.Is(err, ErrOIDCDisabled) && !errors.Is(err, ErrOIDCState) && !errors.Is(err, ErrOIDCNoAccount) &&
		!errors.Is(err, ErrOIDCNotInGroup) && !errors.Is(err, ErrOIDCLinked) && !errors.Is(err, ErrUserDisabled) &&
		!errors.As(err, &rl) && !strings.HasPrefix(msg, "the provider refused") {
		// Details of other failures are in the server log.
		msg = "single sign-on failed; the server log has the details"
	}
	v := url.Values{"sso_error": {truncate(msg, 300)}}
	if redirect != "" && redirect != "/" {
		v.Set("redirect", redirect)
	}
	continueTo(w, s.appURL("/login")+"?"+v.Encode())
}

// continueTo answers with a page that goes on to target (a path of this server).
func continueTo(w http.ResponseWriter, target string) {
	t := html.EscapeString(target)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=` + t +
		`"><title>nerdui</title></head><body><p><a href="` + t + `">Continue to nerdui</a></p></body></html>`))
}

func (s *Service) handleGetOIDC(w http.ResponseWriter, r *http.Request) {
	c, err := s.OIDCSettings(r.Context())
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, OIDCSettingsResponse{OIDCSettings: c, CallbackURL: s.callbackURL(r)})
}

func (s *Service) handlePutOIDC(w http.ResponseWriter, r *http.Request) {
	var req OIDCSettings
	if !httpjson.Decode(w, r, &req) {
		return
	}
	// Whoever sets the role of new accounts must have its actions (ADR-0058).
	if err := s.mayGrant(r.Context(), s.store.Read(), s.can(r), cmp.Or(strings.TrimSpace(req.Role), authz.RoleViewer)); s.writeAuthError(w, err) {
		return
	}
	c, err := s.SetOIDCSettings(r.Context(), req)
	if errors.Is(err, ErrInvalidOIDC) {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
		return
	}
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.opts.Logger.Info("single sign-on settings saved", "enabled", c.Enabled, "issuer", c.Issuer)
	httpjson.Write(w, http.StatusOK, OIDCSettingsResponse{OIDCSettings: c, CallbackURL: s.callbackURL(r)})
}

func (s *Service) handleUnlinkOIDC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeAuthError(w, ErrUserNotFound)
		return
	}
	if !s.mayManageUser(w, r, id) {
		return
	}
	u, err := s.UnlinkOIDC(r.Context(), id)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, u)
}
