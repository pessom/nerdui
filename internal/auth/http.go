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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/proxy"
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

// SubjectFromRequest adapts the request's principal for the authorizer (authz.SubjectFunc).
func SubjectFromRequest(r *http.Request) (authz.Subject, bool) {
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		return authz.Subject{}, false
	}
	s := authz.Subject{UserID: p.User.ID, Username: p.User.Username, Role: p.User.Role}
	if p.APIKey != nil {
		s.APIKey, s.ReadOnly, s.Permissions = true, p.APIKey.ReadOnly, p.APIKey.Permissions
		s.KeyName = p.APIKey.Name + " (" + p.APIKey.Prefix + "…)"
	}
	return s, true
}

// bearer returns the token of an "Authorization: Bearer" header.
func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

// OptionalSession attaches the principal when a valid session cookie is present and
// otherwise passes the request through unchanged.
func (s *Service) OptionalSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearer(r); ok {
			if p, err := s.AuthenticateKey(r.Context(), token); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
		} else if c, err := r.Cookie(CookieName); err == nil {
			if p, err := s.Authenticate(r.Context(), c.Value); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSession rejects requests without a valid session or API key with 401. An
// "Authorization: Bearer" header is an API key (#87); otherwise the session cookie is used.
func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearer(r); ok {
			p, err := s.AuthenticateKey(r.Context(), token)
			if errors.Is(err, ErrUnauthenticated) {
				httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "invalid, expired or revoked API key")
				return
			}
			if err != nil {
				httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "authentication failed")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
			return
		}
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, "authentication required")
			return
		}
		p, err := s.Authenticate(r.Context(), c.Value)
		if errors.Is(err, ErrUnauthenticated) {
			s.clearCookie(w, r)
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
		// A bearer key is never sent by a browser on its own: there is nothing to forge.
		if p.APIKey != nil {
			next.ServeHTTP(w, r)
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

// PasswordChanged rejects requests of sessions that still have to replace a temporary
// password (#74) with 403 password_change_required. Must run after RequireSession; only the
// caller's own session endpoints (me, logout, password) are exempt.
func PasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := PrincipalFrom(r.Context()); ok && p.User.MustChangePassword {
			httpjson.WriteError(w, http.StatusForbidden, httpjson.CodePasswordChangeRequired, ErrPasswordChange.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Routes registers the authentication and user management endpoints under the API root.
func (s *Service) Routes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/setup", authz.Public(s.handleSetupStatus))
	r.Method(http.MethodPost, "/setup", authz.Public(s.handleSetup))
	r.Method(http.MethodPost, "/auth/login", authz.Public(s.handleLogin))
	r.Method(http.MethodPost, "/auth/login/mfa", authz.Public(s.handleLoginMFA))
	s.passkeyPublicRoutes(r)
	s.oidcPublicRoutes(r)
	r.Group(func(r chi.Router) {
		r.Use(s.RequireSession, CSRF)
		r.Method(http.MethodGet, "/auth/me", g.Protect(authz.SessionSelf, s.handleMe))
		r.Method(http.MethodPost, "/auth/logout", g.Protect(authz.SessionSelf, s.handleLogout))
		r.Method(http.MethodPost, "/auth/password", g.Protect(authz.SessionSelf, s.handleChangePassword))
		r.Group(func(r chi.Router) {
			r.Use(PasswordChanged)
			s.mfaSessionRoutes(r, g)
			s.passkeySessionRoutes(r, g)
		})
		r.Group(func(r chi.Router) {
			r.Use(PasswordChanged, s.MFAEnrolled)
			s.mfaAdminRoutes(r, g)
			s.oidcAdminRoutes(r, g)
			s.userRoutes(r, g)
			s.apiKeyRoutes(r, g)
			s.preferenceRoutes(r, g)
		})
	})
}

// SessionResponse is returned by setup, login and me. Keep in sync with api/openapi.yaml.
type SessionResponse struct {
	User      User      `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
	// MFASetupRequired: two-factor authentication is required and the user has none yet (#75).
	MFASetupRequired bool `json:"mfaSetupRequired"`
}

func (s *Service) sessionResponse(r *http.Request, u User, csrf string, expires time.Time) SessionResponse {
	return SessionResponse{
		User: u, CSRFToken: csrf, ExpiresAt: expires,
		MFASetupRequired: !u.MFAEnabled && !viaSSO(r) && s.RequireMFA(r.Context()),
	}
}

// viaSSO reports whether the request's session signed in through the single sign-on
// provider, which is responsible for the second factor then (#76).
func viaSSO(r *http.Request) bool {
	p, ok := PrincipalFrom(r.Context())
	return ok && p.APIKey == nil && p.Method == MethodOIDC
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
	s.audit(r, "auth:setup", req.Username, err)
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, r, issued)
	httpjson.Write(w, http.StatusCreated, s.sessionResponse(r, u, issued.CSRFToken, issued.ExpiresAt))
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
	var mfa *MFAChallengeError
	if errors.As(err, &mfa) {
		// The second step is audited: the password alone did not sign in.
		httpjson.Write(w, http.StatusOK, MFAChallengeResponse{MFARequired: true, MFAToken: mfa.Token, TOTP: mfa.TOTP, Passkey: mfa.Passkey})
		return
	}
	s.audit(r, "auth:login", req.Username, err)
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, r, issued)
	httpjson.Write(w, http.StatusOK, s.sessionResponse(r, u, issued.CSRFToken, issued.ExpiresAt))
}

func (s *Service) audit(r *http.Request, action, username string, err error) {
	if s.opts.Audit == nil {
		return
	}
	if len(username) > 100 {
		username = username[:100]
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.opts.Audit.Auth(r, action, username, err == nil, msg)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	httpjson.Write(w, http.StatusOK, s.sessionResponse(r, p.User, p.CSRFToken, p.ExpiresAt))
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	if err := s.Logout(r.Context(), p); err != nil {
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "logout failed")
		return
	}
	s.clearCookie(w, r)
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
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidSetupToken),
		errors.Is(err, ErrMFAInvalidCode), errors.Is(err, ErrMFAChallenge):
		httpjson.WriteError(w, http.StatusUnauthorized, httpjson.CodeUnauthenticated, err.Error())
	// A refused passkey is 400 in every step: the browser's answer was wrong, not the session.
	case errors.Is(err, ErrPasskeyFailed), errors.Is(err, ErrPasskeyCeremony), errors.Is(err, ErrPasskeyName):
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
	case errors.Is(err, ErrPasskeyNotFound):
		httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, err.Error())
	case errors.Is(err, ErrPasskeyHost), errors.Is(err, ErrPasskeyLimit):
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, err.Error())
	case errors.Is(err, ErrUserDisabled):
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden, err.Error())
	case errors.Is(err, ErrUserNotFound):
		httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, err.Error())
	case errors.Is(err, ErrSetupCompleted), errors.Is(err, ErrUsernameTaken),
		errors.Is(err, ErrOwnAccount), errors.Is(err, ErrLastAdmin),
		errors.Is(err, ErrMFANotStarted), errors.Is(err, ErrMFANotEnabled), errors.Is(err, ErrMFAAlreadyEnabled):
		httpjson.WriteError(w, http.StatusConflict, httpjson.CodeConflict, err.Error())
	// A wrong current password is not 401: the session itself is valid.
	case errors.Is(err, ErrWeakPassword), errors.Is(err, ErrInvalidUsername),
		errors.Is(err, ErrWrongPassword), errors.Is(err, ErrSamePassword):
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
	default:
		s.opts.Logger.Error("authentication error", "error", err)
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
	}
	return true
}

// Secure is on with TLS, and behind a trusted reverse proxy that reports HTTPS (ADR-0005, #112).
func (s *Service) secure(r *http.Request) bool { return s.opts.SecureCookie || proxy.Secure(r) }

func (s *Service) cookiePath() string {
	if s.opts.CookiePath == "" {
		return "/"
	}
	return s.opts.CookiePath
}

func (s *Service) setCookie(w http.ResponseWriter, r *http.Request, is Issued) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name:     CookieName,
		Value:    is.Token,
		Path:     s.cookiePath(),
		Expires:  is.ExpiresAt,
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Service) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the TLS setting
		Name:     CookieName,
		Value:    "",
		Path:     s.cookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// clientOf extracts the client address; behind a trusted proxy, proxy.Middleware has already
// replaced RemoteAddr with the client's (#112).
func clientOf(r *http.Request) Client {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return Client{IP: ip, UserAgent: r.UserAgent()}
}
