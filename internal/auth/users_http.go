// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
)

// userRoutes registers user management (#74, ADR-0016). Callers must be authenticated and
// must not have a pending password change.
func (s *Service) userRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/users", g.Protect(authz.UsersRead, s.handleListUsers))
	r.Method(http.MethodPost, "/users", g.Protect(authz.UsersWrite, s.handleCreateUser))
	r.Method(http.MethodGet, "/users/{id}", g.Protect(authz.UsersRead, s.handleGetUser))
	r.Method(http.MethodPatch, "/users/{id}", g.Protect(authz.UsersWrite, s.handleUpdateUser))
	r.Method(http.MethodPost, "/users/{id}/password", g.Protect(authz.UsersWrite, s.handleSetUserPassword))
	r.Method(http.MethodDelete, "/users/{id}", g.Protect(authz.UsersWrite, s.handleDeleteUser))
}

// UserPasswordResponse carries a generated temporary password; it is shown once.
type UserPasswordResponse struct {
	User              *UserDetail `json:"user,omitempty"`
	GeneratedPassword string      `json:"generatedPassword,omitempty"`
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.ListUsers(r.Context())
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"users": us})
}

func (s *Service) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	u, err := s.GetUser(r.Context(), id)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, u)
}

func (s *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	u, generated, err := s.CreateUser(r.Context(), req.Username, req.Password)
	if s.writeAuthError(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusCreated, UserPasswordResponse{User: &u, GeneratedPassword: generated})
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var req struct {
		Disabled *bool `json:"disabled"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if req.Disabled == nil {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "nothing to update: set disabled")
		return
	}
	u, err := s.SetDisabled(r.Context(), actorID(r), id, *req.Disabled)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, u)
}

func (s *Service) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	generated, err := s.SetTemporaryPassword(r.Context(), actorID(r), id, req.Password)
	if s.writeAuthError(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpjson.Write(w, http.StatusOK, UserPasswordResponse{GeneratedPassword: generated})
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if s.writeAuthError(w, s.DeleteUser(r.Context(), actorID(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	if s.writeAuthError(w, s.ChangePassword(r.Context(), p, req.CurrentPassword, req.NewPassword)) {
		return
	}
	p.User.MustChangePassword = false
	httpjson.Write(w, http.StatusOK, SessionResponse{User: p.User, CSRFToken: p.CSRFToken, ExpiresAt: p.ExpiresAt})
}

func userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "user id must be a positive integer")
		return 0, false
	}
	return id, true
}

func actorID(r *http.Request) int64 {
	p, _ := PrincipalFrom(r.Context())
	return p.User.ID
}
