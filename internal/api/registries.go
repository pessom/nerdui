// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// listRegistries returns the registries the host is logged in to (#51):
//
//	GET /api/v1/environments/{env}/registries
func (h *Handler) listRegistries(w http.ResponseWriter, r *http.Request) {
	list, err := EnvFrom(r.Context()).Runtime.Registries().List(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"registries": list})
}

// loginRegistry verifies credentials with the registry and stores them like `nerdctl login`:
//
//	POST /api/v1/environments/{env}/registries  {"server": "...", "username": "...", "password": "..."}
func (h *Handler) loginRegistry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpjson.Decode(w, r, &req) {
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	res, err := EnvFrom(r.Context()).Runtime.Registries().Login(ctx, req.Server, req.Username, req.Password)
	if err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	httpjson.Write(w, http.StatusCreated, res)
}

// logoutRegistry removes stored credentials like `nerdctl logout`:
//
//	DELETE /api/v1/environments/{env}/registries?server=registry.local:5000
func (h *Handler) logoutRegistry(w http.ResponseWriter, r *http.Request) {
	if err := EnvFrom(r.Context()).Runtime.Registries().Logout(r.Context(), r.URL.Query().Get("server")); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
