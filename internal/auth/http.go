// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
)

// CookieName is the session cookie (ADR-0005).
const CookieName = "nerdui_session"

// CSRFHeader carries the per-session CSRF token on unsafe requests.
const CSRFHeader = "X-CSRF-Token"

type ctxKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the authenticated principal, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// OptionalSession attaches the principal when a valid session cookie is present and
// otherwise passes the request through unchanged.
func (s *Service) OptionalSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(CookieName); err == nil {
			if p, err := s.Authenticate(r.Context(), c.Value); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSession rejects requests without a valid session with 401.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "authentication required")
			return
		}
		p, err := s.Authenticate(r.Context(), c.Value)
		if errors.Is(err, ErrUnauthenticated) {
			s.clearCookie(w)
			httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "session expired or invalid")
			return
		}
		if err != nil {
			httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "authentication failed")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// CSRF protects unsafe methods of authenticated routes: the X-CSRF-Token header must match
// the session's token, and a present Origin header must match the request host.
// Must run after RequireSession.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "authentication required")
			return
		}
		if !SameOrigin(r) {
			httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
			return
		}
		got := r.Header.Get(CSRFHeader)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRFToken)) != 1 {
			httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "missing or invalid "+CSRFHeader+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SameOrigin reports whether the request's Origin header (if present) matches its Host.
// Browsers always send Origin on cross-origin and unsafe requests.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// Routes registers the authentication endpoints under the API root.
func (s *Service) Routes(r chi.Router) {
	r.Get("/setup", s.handleSetupStatus)
	r.Post("/setup", s.handleSetup)
	r.Post("/auth/login", s.handleLogin)
	r.Group(func(r chi.Router) {
		r.Use(s.RequireSession, CSRF)
		r.Get("/auth/me", s.handleMe)
		r.Post("/auth/logout", s.handleLogout)
	})
}

// SessionResponse is returned by setup, login and me. Keep in sync with api/openapi.yaml.
type SessionResponse struct {
	User      User      `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Service) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]bool{"required": s.SetupRequired()})
}

func (s *Service) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	var req struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	u, issued, err := s.Setup(r.Context(), req.Token, req.Username, req.Password, clientOf(r))
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, issued)
	httpjson.Write(w, http.StatusCreated, SessionResponse{User: u, CSRFToken: issued.CSRFToken, ExpiresAt: issued.ExpiresAt})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	u, issued, err := s.Login(r.Context(), req.Username, req.Password, clientOf(r))
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, issued)
	httpjson.Write(w, http.StatusOK, SessionResponse{User: u, CSRFToken: issued.CSRFToken, ExpiresAt: issued.ExpiresAt})
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	httpjson.Write(w, http.StatusOK, SessionResponse{User: p.User, CSRFToken: p.CSRFToken, ExpiresAt: p.ExpiresAt})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if err := s.Logout(r.Context(), p); err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "logout failed")
		return
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// writeAuthError maps service errors to responses; it returns false when err is nil.
func (s *Service) writeAuthError(w http.ResponseWriter, err error) bool {
	var rl *RateLimitedError
	switch {
	case err == nil:
		return false
	case errors.As(err, &rl):
		httpjson.WriteRateLimited(w, rl.RetryAfter)
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidSetupToken):
		httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, err.Error())
	case errors.Is(err, ErrSetupCompleted):
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, err.Error())
	case errors.Is(err, ErrWeakPassword), errors.Is(err, ErrInvalidUsername):
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
	default:
		s.opts.Logger.Error("authentication error", "error", err)
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
	}
	return true
}

// Secure is only off with --tls=false, i.e. behind a TLS-terminating reverse proxy (ADR-0005).
func (s *Service) setCookie(w http.ResponseWriter, is Issued) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name:     CookieName,
		Value:    is.Token,
		Path:     "/",
		Expires:  is.ExpiresAt,
		HttpOnly: true,
		Secure:   s.opts.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Service) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.opts.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

// clientOf extracts the client address. Proxy headers are not trusted until the
// reverse-proxy mode (#112) adds an explicit trusted-proxies setting.
func clientOf(r *http.Request) Client {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return Client{IP: ip, UserAgent: r.UserAgent()}
}
