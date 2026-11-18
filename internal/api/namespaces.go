// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// NamespaceSpec creates a namespace (#195).
type NamespaceSpec struct {
	Name string `json:"name"`
}

// POST /api/v1/environments/{env}/namespaces  NamespaceSpec
func (h *Handler) createNamespace(w http.ResponseWriter, r *http.Request) {
	var spec NamespaceSpec
	if !httpjson.Decode(w, r, &spec) {
		return
	}
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	e := EnvFrom(r.Context())
	if err := e.Runtime.System().CreateNamespace(ctx, spec.Name); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.log().Info("namespace created", "environment", e.ID, "namespace", spec.Name)
	httpjson.Write(w, http.StatusCreated, spec)
}

// DELETE /api/v1/environments/{env}/namespaces/{name}
func (h *Handler) removeNamespace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := runtime.ActionContext(r.Context())
	defer cancel()
	e := EnvFrom(r.Context())
	name := chi.URLParam(r, "name")
	if err := e.Runtime.System().RemoveNamespace(ctx, name); err != nil {
		h.writeActionErr(w, r, err)
		return
	}
	h.log().Info("namespace removed", "environment", e.ID, "namespace", name)
	w.WriteHeader(http.StatusNoContent)
}
