// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// PasskeyOptions are the options for navigator.credentials.create() or .get() (their JSON
// form, with a "publicKey" member) and the token of the request. Keep in sync with
// api/openapi.yaml.
type PasskeyOptions struct {
	Token   string `json:"token,omitempty"`
	Options any    `json:"options"`
}

// PasskeyAdded is the new passkey, with backup codes when it is the user's first second
// factor. Keep in sync with api/openapi.yaml.
type PasskeyAdded struct {
	Passkey     Passkey  `json:"passkey"`
	BackupCodes []string `json:"backupCodes,omitempty"`
}

// passkeyPublicRoutes sign in with a passkey.
func (s *Service) passkeyPublicRoutes(r chi.Router) {
	r.Method(http.MethodPost, "/auth/passkey/options", authz.Public(s.handlePasskeyLoginOptions))
	r.Method(http.MethodPost, "/auth/passkey/login", authz.Public(s.handlePasskeyLogin))
	r.Method(http.MethodPost, "/auth/login/mfa/passkey", authz.Public(s.handlePasskeyMFAOptions))
}

// passkeySessionRoutes manage the caller's passkeys; available before two-factor setup, as a
// passkey is one.
func (s *Service) passkeySessionRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/auth/passkeys", g.Protect(authz.SessionSelf, s.handleListPasskeys))
	r.Method(http.MethodPost, "/auth/passkeys/options", g.Protect(authz.SessionSelf, s.handlePasskeyRegisterOptions))
	r.Method(http.MethodPost, "/auth/passkeys", g.Protect(authz.SessionSelf, s.handleAddPasskey))
	r.Method(http.MethodDelete, "/auth/passkeys/{id}", g.Protect(authz.SessionSelf, s.handleDeletePasskey))
}

// relyingPartyOf is the host the browser opened nerdui by (from a trusted proxy, #112).
func (s *Service) relyingPartyOf(r *http.Request) (relyingParty, error) {
	return newRelyingParty(r.Host, r.TLS != nil || s.secure(r))
}

func (s *Service) handlePasskeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	rp, err := s.relyingPartyOf(r)
	if s.writeAuthError(w, err) {
		return
	}
	token, opts, err := s.BeginPasskeyLogin(rp, clientOf(r))
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, PasskeyOptions{Token: token, Options: opts})
}

func (s *Service) handlePasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	var req struct {
		Token      string          `json:"token"`
		Credential json.RawMessage `json:"credential"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	u, issued, err := s.FinishPasskeyLogin(r.Context(), req.Token, req.Credential, clientOf(r))
	s.audit(r, "auth:login-passkey", u.Username, err)
	if s.writeAuthError(w, err) {
		return
	}
	s.setCookie(w, r, issued)
	httpjson.Write(w, http.StatusOK, s.sessionResponse(r, u, issued.CSRFToken, issued.ExpiresAt))
}

func (s *Service) handlePasskeyMFAOptions(w http.ResponseWriter, r *http.Request) {
	if !SameOrigin(r) {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeCSRF, "cross-origin request rejected")
		return
	}
	var req struct {
		Token string `json:"mfaToken"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	rp, err := s.relyingPartyOf(r)
	if s.writeAuthError(w, err) {
		return
	}
	opts, err := s.BeginPasskeyMFA(r.Context(), req.Token, rp)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, PasskeyOptions{Options: opts})
}

func (s *Service) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	keys, err := s.Passkeys(r.Context(), p.User.ID)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, keys)
}

func (s *Service) handlePasskeyRegisterOptions(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	rp, err := s.relyingPartyOf(r)
	if s.writeAuthError(w, err) {
		return
	}
	token, opts, err := s.BeginPasskeyRegistration(r.Context(), p.User.ID, req.Password, rp)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, PasskeyOptions{Token: token, Options: opts})
}

func (s *Service) handleAddPasskey(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Token      string          `json:"token"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	key, codes, err := s.FinishPasskeyRegistration(r.Context(), p.User.ID, req.Token, req.Name, req.Credential)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusCreated, PasskeyAdded{Passkey: key, BackupCodes: codes})
}

func (s *Service) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.writeAuthError(w, ErrPasskeyNotFound)
		return
	}
	var req passwordRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if s.writeAuthError(w, s.DeletePasskey(r.Context(), p.User.ID, id, req.Password)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
