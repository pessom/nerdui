// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

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
