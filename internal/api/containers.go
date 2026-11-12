// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
)

// listContainers returns all containers of the namespace (#10):
//
//	GET /api/v1/environments/{env}/containers?namespace=default
//
// Filtering and sorting happen client-side (ADR-0006: home-scale collections).
func (h *Handler) listContainers(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := EnvFrom(r.Context()).Runtime.Containers().List(r.Context(), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "containers": list})
}

// inspectContainer returns the container as `nerdctl inspect` shows it (#13):
//
//	GET /api/v1/environments/{env}/containers/{id}/inspect?namespace=default&size=false
//
// {id} is a full or short ID or a name. size also computes layer sizes (slow).
func (h *Handler) inspectContainer(w http.ResponseWriter, r *http.Request) {
	size, err := boolParam(r.URL.Query().Get("size"))
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, err.Error())
		return
	}
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	v, err := EnvFrom(r.Context()).Runtime.Containers().Inspect(r.Context(), ns, chi.URLParam(r, "id"), size)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, v)
}
