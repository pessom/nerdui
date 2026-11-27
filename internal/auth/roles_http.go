// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/store"
)

// roleRoutes manage custom roles (#78, ADR-0058); like users, with users:read and users:write.
func (s *Service) roleRoutes(r chi.Router, g *authz.Guard) {
	r.Method(http.MethodGet, "/roles", g.Protect(authz.UsersRead, s.handleListRoles))
	r.Method(http.MethodPost, "/roles", g.Protect(authz.UsersWrite, s.handleCreateRole))
	r.Method(http.MethodPut, "/roles/{name}", g.Protect(authz.UsersWrite, s.handleUpdateRole))
	r.Method(http.MethodDelete, "/roles/{name}", g.Protect(authz.UsersWrite, s.handleDeleteRole))
}

// can reports which actions the caller of r may perform: their role, limited by their API key.
func (s *Service) can(r *http.Request) func(authz.Action) bool {
	return func(a authz.Action) bool { return s.guard != nil && s.guard.Allowed(r, a) }
}

// mayManageUser writes 403 unless the caller has every action of user id's role; a missing
// user is left to the handler.
func (s *Service) mayManageUser(w http.ResponseWriter, r *http.Request, id int64) bool {
	u, err := s.store.Read().GetUserByID(r.Context(), id)
	if store.IsNotFound(err) {
		return true
	}
	if err == nil {
		err = s.mayManage(r.Context(), s.can(r), u)
	}
	return !s.writeAuthError(w, err)
}

type roleRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Permissions []authz.Action `json:"permissions"`
}

func (s *Service) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.ListRoles(r.Context())
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, roles)
}

func (s *Service) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	role, err := s.CreateRole(r.Context(), s.can(r), req.Name, req.Description, req.Permissions)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusCreated, role)
}

func (s *Service) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if !httpjson.Decode(w, r, &req) {
		return
	}
	role, err := s.UpdateRole(r.Context(), s.can(r), chi.URLParam(r, "name"), req.Description, req.Permissions)
	if s.writeAuthError(w, err) {
		return
	}
	httpjson.Write(w, http.StatusOK, role)
}

func (s *Service) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if s.writeAuthError(w, s.DeleteRole(r.Context(), s.can(r), chi.URLParam(r, "name"))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
