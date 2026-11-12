// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/pessom/nerdui/internal/httpjson"
)

// listImages returns the images of a namespace (#42):
//
//	GET /api/v1/environments/{env}/images?namespace=default
func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := EnvFrom(r.Context()).Runtime.Images().List(r.Context(), ns)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"namespace": ns, "images": list})
}
