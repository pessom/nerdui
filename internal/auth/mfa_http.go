// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// CodeMFASetupRequired: the administrator requires two-factor authentication and the user has
// not set it up; only the session and two-factor endpoints are available.
const CodeMFASetupRequired = "mfa_setup_required"

// MFAChallengeResponse is returned by login when a second factor is needed. Keep in sync with
// api/openapi.yaml.
type MFAChallengeResponse struct {
	MFARequired bool   `json:"mfaRequired"`
	MFAToken    string `json:"mfaToken"`
	// TOTP and Passkey tell which factors the user has; backup codes always work.
	TOTP    bool `json:"totp"`
	Passkey bool `json:"passkey"`
}

// SecuritySettings are the server-wide sign-in rules. Keep in sync with api/openapi.yaml.
type SecuritySettings struct {
	RequireMFA bool `json:"requireMfa"`
}

// MFAEnrolled blocks sessions without two-factor authentication while it is required. API keys
// are not affected: they are a separate credential; nor are single sign-on sessions, whose
// provider checks the second factor (#76).
func (s *Service) MFAEnrolled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := PrincipalFrom(r.Context()); ok && p.APIKey == nil && p.Method != MethodOIDC && !p.User.MFAEnabled && s.RequireMFA(r.Context()) {
			httpjson.WriteError(w, http.StatusForbidden, CodeMFASetupRequired, ErrMFASetupRequired.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mfaSessionRoutes are available to every session, also before two-factor setup.
func (s *Service) mfaSessionRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/auth/mfa", g.Protect(authz.SessionSelf, s.handleMFAStatus))
	r.Method(http.MethodPost, "/auth/mfa/setup", g.Protect(authz.SessionSelf, s.handleMFASetup))
	r.Method(http.MethodPost, "/auth/mfa/enable", g.Protect(authz.SessionSelf, s.handleMFAEnable))
	r.Method(http.MethodPost, "/auth/mfa/backup-codes", g.Protect(authz.SessionSelf, s.handleMFABackupCodes))
	r.Method(http.MethodPost, "/auth/mfa/disable", g.Protect(authz.SessionSelf, s.handleMFADisable))
}

// mfaAdminRoutes manage other users and the requirement.
func (s *Service) mfaAdminRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodPost, "/users/{id}/mfa/reset", g.Protect(authz.UsersWrite, s.handleMFAReset))
	r.Method(http.MethodGet, "/settings/security", g.Protect(authz.SettingsRead, s.handleGetSecurity))
	r.Method(http.MethodPut, "/settings/security", g.Protect(authz.SettingsWrite, s.handlePutSecurity))
}

func (s *Service) handleLoginMFA(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	var req struct {
		Token string `json:"mfaToken"`
		Code  string `json:"code"`
		// Passkey is the browser's answer to the options of /auth/login/mfa/passkey.
		Passkey json.RawMessage `json:"passkey"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	u, issued, err := s.LoginMFA(r.Context(), req.Token, req.Code, req.Passkey, clientOf(r))
	s.audit(r, "auth:login", u.Username, err)
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, r, issued)
	httpjson.Write(w, http.StatusOK, s.sessionResponse(r, u, issued.CSRFToken, issued.ExpiresAt))
}

func (s *Service) handleMFAStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	st, err := s.MFAStatusOf(r.Context(), p.User.ID)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, st)
}

type passwordRequest struct {
	Password string `json:"password"`
}

func (s *Service) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	e, err := s.StartMFA(r.Context(), p.User.ID, req.Password)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, e)
}

func (s *Service) handleMFAEnable(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	codes, err := s.EnableMFA(r.Context(), p.User.ID, req.Code)
	if errors.Is(err, ErrMFAInvalidCode) {
		// The session is valid: a wrong code here is a bad request, not 401.
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
		return
	}
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, map[string][]string{"backupCodes": codes})
}

func (s *Service) handleMFABackupCodes(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	codes, err := s.NewBackupCodes(r.Context(), p.User.ID, req.Password)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, map[string][]string{"backupCodes": codes})
}

func (s *Service) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if s.writeAuthError(w, s.DisableMFA(r.Context(), p.User.ID, req.Password)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleMFAReset(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if s.writeAuthError(w, s.ResetMFA(r.Context(), p.User.ID, id)) {
		return
	}
	s.opts.Logger.Info("two-factor authentication reset", "user_id", id, "by", p.User.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleGetSecurity(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, SecuritySettings{RequireMFA: s.RequireMFA(r.Context())})
}

func (s *Service) handlePutSecurity(w http.ResponseWriter, r *http.Request) {
	var req SecuritySettings
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if err := s.SetRequireMFA(r.Context(), req.RequireMFA); err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.opts.Logger.Info("security settings saved", "require_mfa", req.RequireMFA)
	httpjson.Write(w, http.StatusOK, req)
}
