// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// apiKeyRoutes registers the API key endpoints (#87): a user's own keys under /auth, and the
// keys of any user for administrators under /users.
func (s *Service) apiKeyRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/auth/api-keys", g.Protect(authz.SessionSelf, s.handleListOwnKeys))
	r.Method(http.MethodPost, "/auth/api-keys", g.Protect(authz.SessionSelf, s.handleCreateKey(g)))
	r.Method(http.MethodDelete, "/auth/api-keys/{key}", g.Protect(authz.SessionSelf, s.handleDeleteOwnKey))
	r.Method(http.MethodGet, "/users/{id}/api-keys", g.Protect(authz.UsersRead, s.handleListUserKeys))
	r.Method(http.MethodDelete, "/users/{id}/api-keys/{key}", g.Protect(authz.UsersWrite, s.handleDeleteUserKey))
}

// CreatedAPIKey is the response to creating a key: the key itself is shown only here.
type CreatedAPIKey struct {
	APIKey
	Key string `json:"key"`
}

// passwordSession rejects API key principals: keys cannot manage keys or the account.
func passwordSession(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, _ := PrincipalFrom(r.Context())
	if p.APIKey != nil {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden, ErrNotWithAPIKey.Error())
		return Principal{}, false
	}
	return p, true
}

func keyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "key"), 10, 64)
	if err != nil || id <= 0 {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "key id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Service) writeKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidAPIKey):
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
	case errors.Is(err, ErrAPIKeyNotFound), errors.Is(err, ErrUserNotFound):
		httpjson.WriteError(w, http.StatusNotFound, httpjson.CodeNotFound, err.Error())
	default:
		s.opts.Logger.Error("API key error", "error", err)
		httpjson.WriteError(w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
	}
}

// GET /api/v1/auth/api-keys
func (s *Service) handleListOwnKeys(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	keys, err := s.ListAPIKeys(r.Context(), p.User.ID)
	if err != nil {
		s.writeKeyError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"keys": keys})
}

// POST /api/v1/auth/api-keys  {"name", "readOnly", "expiresAt"?, "permissions"?}
//
// A key may only get permissions its creator has (#80).
func (s *Service) handleCreateKey(g *authz.Guard) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := passwordSession(w, r)
		if !ok {
			return
		}
		var req struct {
			Name        string     `json:"name"`
			ReadOnly    bool       `json:"readOnly"`
			ExpiresAt   *time.Time `json:"expiresAt"`
			Permissions []string   `json:"permissions"`
		}
		if !httpjson.Decode(w, r, &req) {
			return
		}
		for _, perm := range req.Permissions {
			if a := authz.Action(perm); authz.Known(a) && !g.Allowed(r, a) {
				httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden,
					fmt.Sprintf("you may not %s, so your API keys may not either", a))
				return
			}
		}
		k, token, err := s.CreateAPIKey(r.Context(), p.User.ID, req.Name, KeyOptions{
			ReadOnly: req.ReadOnly, ExpiresAt: req.ExpiresAt, Permissions: req.Permissions,
		})
		if err != nil {
			s.writeKeyError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		httpjson.Write(w, http.StatusCreated, CreatedAPIKey{APIKey: k, Key: token})
	}
}

// DELETE /api/v1/auth/api-keys/{key}
func (s *Service) handleDeleteOwnKey(w http.ResponseWriter, r *http.Request) {
	p, ok := passwordSession(w, r)
	if !ok {
		return
	}
	id, ok := keyID(w, r)
	if !ok {
		return
	}
	if err := s.DeleteAPIKey(r.Context(), p.User.ID, id); err != nil {
		s.writeKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/users/{id}/api-keys
func (s *Service) handleListUserKeys(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if _, err := s.GetUser(r.Context(), id); err != nil {
		s.writeKeyError(w, err)
		return
	}
	keys, err := s.ListAPIKeys(r.Context(), id)
	if err != nil {
		s.writeKeyError(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"keys": keys})
}

// DELETE /api/v1/users/{id}/api-keys/{key}
func (s *Service) handleDeleteUserKey(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	kid, ok := keyID(w, r)
	if !ok {
		return
	}
	if err := s.DeleteAPIKey(r.Context(), id, kid); err != nil {
		s.writeKeyError(w, err)
		return
	}
	s.opts.Logger.Info("API key revoked by an administrator", "actor_id", actorID(r), "user_id", id, "key_id", kid)
	w.WriteHeader(http.StatusNoContent)
}
