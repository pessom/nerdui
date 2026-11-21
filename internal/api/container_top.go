// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/httpjson"
)

// containerTop lists the processes of a running container (#20):
//
//	GET /api/v1/environments/{env}/containers/{id}/top?namespace=
func (h *Handler) containerTop(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	procs, err := EnvFrom(r.Context()).Runtime.Containers().Top(r.Context(), ns, chi.URLParam(r, "id"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"processes": procs})
}
